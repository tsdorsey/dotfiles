theBeastCodes =
  `AG1,AG2,AJ1,ALO,AN3,AP9,AS3,AT5,AU0,AXP,A31,BVE,B30,B39,B58,B71,C69,DF5,B85,CJ2,C25,C36,C49,C5W,DH2,DK7,DL3,DT4,007,EVA,E52,FK2,FK3,GT5,G65,G80,JAN,JC4,JF4,KG3,KNP,KUP,K34,LM7,M30,NC8,NP5,PDC,POZ,PF4,QMJ,RYJ,R7D,SAF,SLM,SLT,T96,UC6,UE1,UK3,UK6,U07,VB3,VG3,VG8,VP6,VR4,VYS,V1K,V22,V54,V73,XMJ,x88,YA7,YE9,YF5,YMJ,ZMJ,2W7,ZY1,282,1SM,1SZ,47U,521,522,6XK,7XK`.split(
    ","
  );
avalanche2004Codes =
  `AC6,AG1,AG2,AJ7,ALO,AN3,AT5,AU0,AXP,A31,B30,B41,B58,CF5,CJ2,C49,DF5,DH6,DK8,DL3,DT3,DT4,D07,EN4,EVA,C5U,FF7,JF4,JH2,KUP,NP5,NU4,PDA,PDE,PDH,PF9,TR3,UC6,UE1,UG1,UK3,YE9,YF5,FF6,GT4,K34,K68,LM7,G80,MSL,M30,QMJ,R6I,R9U,SAF,SLM,T96,UK6,UQ7,U2K,U42,VB3,VFF,VG3,VG8,VP6,VR4,VXS,V1K,V54,V73,V76,WBH,XMJ,X88,YD3,YD6,YMJ,ZMJ,ZM9,701,ZW9,ZY1,Z82,1SM,1SZ,59U,6XL,691,692,7XL`.split(
    ","
  );

(async function () {
  const uniq = (items) => Array.from(new Set(items));

  const getCodeTable = async (code) => {
    const result = await fetch(
      `https://carinspector.us/getRPO.php?RPO=${code}`
    );
    const text = await result.text();

    return text
      .replaceAll("\n", " ")
      .replaceAll("\t", " ")
      .replaceAll(/ *> */g, ">")
      .replaceAll(/ *< */g, "<")
      .replaceAll(/ +/g, " ")
      .match(/<table.*<\/table>/g);
  };

  const parseCodeTable = (tableString, code) => {
    const rows = tableString.match(/(?<=<tr>).+?(?=<\/tr>)/g);
    if (rows === null) {
      console.warn(`${code} did not return any results`);
      return [];
    }

    const descriptions = rows
      .map((row) => row.match(/(?<=<td>).+?(?=<\/td>)/g))
      .filter((row) => row !== null)
      .map((row) => row.map((cell) => cell.trim()))
      .map((row) =>
        row
          .filter((cell) => code.toLowerCase() !== cell.toLowerCase())
          .join(" - ")
      );

    if (descriptions.length < 1) {
      console.warn(`${code} did not return any results`);
    }

    return uniq(descriptions);
  };

  const getCodeDescriptions = async (code) => {
    const table = await getCodeTable(code);
    if (table === null) {
      return [];
    }
    return parseCodeTable(table[0], code);
  };

  const getAllCodeDescriptions = async (codes) => {
    const descriptionBlocks = await Promise.all(
      codes.map((code) => getCodeDescriptions(code))
    );
    const results = [];

    for (let cI = 0; cI < codes.length; cI++) {
      const code = codes[cI];
      const descriptionBlock = descriptionBlocks[cI];

      for (let dI = 0; dI < descriptionBlock.length; dI++) {
        const description = descriptionBlock[dI];
        results.push([code, description]);
      }
    }

    return results;
  };

  const data = await getAllCodeDescriptions(theBeastCodes);
  output = data.map((row) => row.join(","));
  output.unshift("RPO Code,Description");
  console.info(output.join("\n"));
})();
