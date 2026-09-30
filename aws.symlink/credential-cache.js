"use strict";
// Daemon and socket client behind credential-cache.sh. Not meant to be run
// directly; the shell script is the CLI and passes:
//   node credential-cache.js daemon  <profile> <watch-pid>
//   node credential-cache.js request <profile> <ping|print|refresh>
// See credential-cache.sh for the overall design.

const crypto = require("crypto");
const fs = require("fs");
const net = require("net");
const os = require("os");
const path = require("path");
const { execFileSync } = require("child_process");

const [mode, profile, ...rest] = process.argv.slice(2);
const CACHE_DIR = path.join(os.homedir(), ".aws", "cache");
const SCRIPT_PATH = process.env.CREDENTIAL_CACHE_SCRIPT || "credential-cache.sh";
const POLL_MS = 5000;
const REFRESH_WINDOW_MS = 600 * 1000;
const REFRESH_RETRY_MS = 60 * 1000;
const LOCK_TRIES = 100;

const file = (ext) => path.join(CACHE_DIR, `${profile}.${ext}`);
const paths = {
  json: file("json"),
  config: file("config"),
  pids: file("pids"),
  daemonPid: file("daemon-pid"),
  sock: file("sock"),
  log: file("log"),
  lock: file("lock"),
  error: file("error"),
};

const sleepSync = (ms) => Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);

const pidAlive = (pid) => {
  if (!/^[1-9][0-9]*$/.test(String(pid))) return false;
  try {
    process.kill(Number(pid), 0);
    return true;
  } catch (error) {
    return error.code === "EPERM";
  }
};

const log = (message) => {
  fs.mkdirSync(CACHE_DIR, { recursive: true, mode: 0o700 });
  const stamp = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
  fs.appendFileSync(paths.log, `${stamp} ${message}\n`, { mode: 0o600 });
};

// ---- socket client -------------------------------------------------------

const request = (command) =>
  new Promise((resolve) => {
    const chunks = [];
    let settled = false;
    const done = (result) => {
      if (!settled) {
        settled = true;
        resolve(result);
      }
    };
    const sock = net.connect(paths.sock);
    sock.on("connect", () => sock.end(`${command}\n`));
    sock.on("data", (chunk) => chunks.push(chunk));
    sock.on("error", (error) => done({ ok: false, body: `${error.code || error.message}\n` }));
    sock.on("close", () => {
      const text = Buffer.concat(chunks).toString("utf8");
      const newline = text.indexOf("\n");
      const status = newline === -1 ? text : text.slice(0, newline);
      const body = newline === -1 ? "" : text.slice(newline + 1);
      done({ ok: status === "ok", body });
    });
  });

if (mode === "request") {
  request(rest[0]).then(({ ok, body }) => {
    (ok ? process.stdout : process.stderr).write(body);
    process.exit(ok ? 0 : 1);
  });
}

// ---- daemon --------------------------------------------------------------

if (mode === "daemon") {
  const watchPid = rest[0];
  const key = crypto.randomBytes(32);
  const state = { region: "", expiresAt: NaN, nextRefreshAttempt: 0, isDaemon: false, server: null };

  const acquireLock = () => {
    for (let tries = 0; ; ) {
      try {
        fs.mkdirSync(paths.lock);
        fs.writeFileSync(path.join(paths.lock, "pid"), `${process.pid}\n`, { mode: 0o600 });
        return;
      } catch (error) {
        if (error.code !== "EEXIST") throw error;
      }
      let owner = "";
      try {
        owner = fs.readFileSync(path.join(paths.lock, "pid"), "utf8").trim();
      } catch {}
      if (!owner || !pidAlive(owner)) {
        fs.rmSync(paths.lock, { recursive: true, force: true });
        continue;
      }
      if (++tries > LOCK_TRIES) throw new Error(`Timed out waiting for credential cache lock ${paths.lock}`);
      sleepSync(100);
    }
  };
  const releaseLock = () => fs.rmSync(paths.lock, { recursive: true, force: true });

  const atomicWrite = (dest, content) => {
    const tmp = path.join(CACHE_DIR, `.tmp.${process.pid}.${crypto.randomBytes(4).toString("hex")}`);
    fs.writeFileSync(tmp, content, { mode: 0o600 });
    fs.renameSync(tmp, dest);
    fs.chmodSync(dest, 0o600);
  };

  const livePids = (extra) => {
    let lines = [];
    try {
      lines = fs.readFileSync(paths.pids, "utf8").split("\n");
    } catch {}
    if (extra !== undefined) lines.push(String(extra));
    const seen = new Set();
    const live = [];
    for (const raw of lines) {
      const pid = raw.trim();
      if (!pid || seen.has(pid)) continue;
      seen.add(pid);
      if (pidAlive(pid)) live.push(pid);
    }
    return live;
  };
  const writePids = (live) => atomicWrite(paths.pids, live.length ? `${live.join("\n")}\n` : "");

  const daemonAlive = () => {
    try {
      return pidAlive(fs.readFileSync(paths.daemonPid, "utf8").trim());
    } catch {
      return false;
    }
  };

  const deleteCache = () => {
    for (const p of [paths.json, paths.config, paths.pids, paths.daemonPid, paths.sock]) {
      fs.rmSync(p, { force: true });
    }
  };

  const encrypt = (plaintext) => {
    const iv = crypto.randomBytes(12);
    const cipher = crypto.createCipheriv("aes-256-gcm", key, iv);
    const ciphertext = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
    return `AWSENC1\n${Buffer.concat([iv, cipher.getAuthTag(), ciphertext]).toString("base64")}\n`;
  };

  const decrypt = (blob) => {
    const body = blob.trim().replace(/^AWSENC1\n?/, "");
    const buf = Buffer.from(body, "base64");
    const decipher = crypto.createDecipheriv("aes-256-gcm", key, buf.subarray(0, 12));
    decipher.setAuthTag(buf.subarray(12, 28));
    try {
      return Buffer.concat([decipher.update(buf.subarray(28)), decipher.final()]).toString("utf8");
    } catch {
      throw new Error("credential cache failed authentication; the file was modified since the daemon wrote it");
    }
  };

  const shellQuote = (value) => `'${String(value).replace(/'/g, `'\\''`)}'`;

  const runAws = (args) => {
    const env = { ...process.env, AWS_SDK_LOAD_CONFIG: "1" };
    for (const name of [
      "AWS_PROFILE",
      "AWS_CONFIG_FILE",
      "AWS_ACCESS_KEY_ID",
      "AWS_SECRET_ACCESS_KEY",
      "AWS_SESSION_TOKEN",
      "AWS_CREDENTIAL_EXPIRATION",
    ]) {
      delete env[name];
    }
    return execFileSync("aws", args, { encoding: "utf8", env, stdio: ["ignore", "pipe", "pipe"] });
  };

  const refresh = () => {
    let raw;
    try {
      raw = runAws(["configure", "export-credentials", "--profile", profile, "--format", "process"]);
    } catch (error) {
      const detail = (error.stderr ? error.stderr.toString() : error.message).trim();
      throw new Error(
        `Failed to export credentials for AWS profile "${profile}".\n` +
          `Ensure the AWS CLI is installed and "aws sts get-caller-identity --profile ${profile}" works.\n${detail}`,
      );
    }
    const region = runAws(["configure", "get", "region", "--profile", profile]).trim();
    if (!region) throw new Error(`Failed to get AWS region for profile "${profile}".`);
    let creds;
    try {
      creds = JSON.parse(raw);
    } catch {
      creds = null;
    }
    if (!creds || creds.Version !== 1 || !creds.AccessKeyId || !creds.SecretAccessKey) {
      throw new Error(`Failed to parse AWS credentials exported for profile "${profile}".`);
    }
    atomicWrite(paths.json, encrypt(JSON.stringify(creds)));
    atomicWrite(
      paths.config,
      `[profile ${profile}_cached]\nregion = ${region}\ncredential_process = ${shellQuote(SCRIPT_PATH)} print ${shellQuote(profile)}\n`,
    );
    state.region = region;
    state.expiresAt = creds.Expiration ? Date.parse(creds.Expiration) : NaN;
  };

  const needsRefresh = () => Number.isFinite(state.expiresAt) && Date.now() >= state.expiresAt - REFRESH_WINDOW_MS;

  const finish = () => {
    if (!state.isDaemon) return;
    state.isDaemon = false;
    if (state.server) state.server.close();
    deleteCache();
  };

  const handle = (sock, command) => {
    try {
      if (command === "ping") return sock.end(`ok\n${state.region}\n`);
      if (command === "print") return sock.end(`ok\n${decrypt(fs.readFileSync(paths.json, "utf8"))}\n`);
      if (command === "refresh") {
        refresh();
        log(`refreshed credential cache for ${profile} on request`);
        return sock.end("ok\n");
      }
      return sock.end(`error\nunknown command "${command}"\n`);
    } catch (error) {
      return sock.end(`error\n${error.message}\n`);
    }
  };

  const listen = () =>
    new Promise((resolve, reject) => {
      fs.rmSync(paths.sock, { force: true });
      const server = net.createServer((sock) => {
        let buffer = "";
        sock.on("error", () => {});
        sock.on("data", (chunk) => {
          buffer += chunk;
          const newline = buffer.indexOf("\n");
          if (newline !== -1) handle(sock, buffer.slice(0, newline).trim());
        });
      });
      server.on("error", reject);
      server.listen(paths.sock, () => {
        fs.chmodSync(paths.sock, 0o600);
        state.server = server;
        resolve();
      });
    });

  const tick = () => {
    acquireLock();
    const live = livePids();
    writePids(live);
    releaseLock();
    if (live.length === 0) {
      log(`removed credential cache for ${profile}; no watched processes remain`);
      process.exit(0);
    }
    const now = Date.now();
    if (needsRefresh() && now >= state.nextRefreshAttempt) {
      try {
        refresh();
        state.nextRefreshAttempt = 0;
        log(`refreshed credential cache for ${profile}`);
      } catch (error) {
        state.nextRefreshAttempt = now + REFRESH_RETRY_MS;
        log(`refresh failed for ${profile}; keeping the previous cache\n${error.message}`);
      }
    }
  };

  (async () => {
    if (!pidAlive(watchPid)) {
      process.stderr.write(`Refusing to watch dead pid ${watchPid} for profile "${profile}".\n`);
      process.exit(1);
    }
    fs.mkdirSync(CACHE_DIR, { recursive: true, mode: 0o700 });
    fs.chmodSync(CACHE_DIR, 0o700);
    process.chdir("/");
    process.on("SIGHUP", () => {});

    acquireLock();
    writePids(livePids(watchPid));
    if (daemonAlive()) {
      releaseLock();
      log(`joined cache daemon for ${profile} watching ${watchPid}`);
      process.exit(0);
    }
    fs.writeFileSync(paths.daemonPid, `${process.pid}\n`, { mode: 0o600 });
    state.isDaemon = true;
    releaseLock();

    process.on("exit", finish);
    process.on("SIGTERM", () => process.exit(0));
    process.on("SIGINT", () => process.exit(0));
    log(`cache daemon started for ${profile} watching ${watchPid}`);

    try {
      refresh();
    } catch (error) {
      log(`initial refresh failed for ${profile}; daemon exiting\n${error.message}`);
      // activate polls for this so it can fail fast instead of waiting for its timeout.
      atomicWrite(paths.error, `${error.message}\n`);
      process.exit(1);
    }
    await listen();
    log(`cache daemon ready for ${profile}`);
    setInterval(tick, POLL_MS);
  })().catch((error) => {
    log(`cache daemon crashed for ${profile}: ${error.stack || error.message}`);
    process.exit(1);
  });
}
