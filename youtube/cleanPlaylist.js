const clickActionMenuButton = () => {
  const actionMenu = document.querySelector(
    "ytd-playlist-video-renderer ytd-menu-renderer button"
  );
  if (actionMenu === null) {
    return false;
  }

  actionMenu.click();
  return true;
};

const clickRemoveButton = (playlistName) => {
  const removeText = `Remove from ${playlistName}`;
  const menuItems = Array.from(
    document.querySelectorAll(
      "ytd-popup-container.ytd-app tp-yt-iron-dropdown yt-formatted-string"
    )
  );

  const removeItem = menuItems.find((node) => {
    if (node.text === undefined) {
      return false;
    }

    const fullText = node.text.runs.map((item) => item.text ?? "").join("");
    if (fullText.toLowerCase().includes(removeText.toLowerCase())) {
      return true;
    }

    return false;
  });

  if (removeItem === undefined) {
    return false;
  }

  removeItem.click();
  return true;
};

const sleep = (ms) => {
  return new Promise((resolve) => setTimeout(resolve, ms));
};

const clean = async (
  playlistName,
  actionFailsLimit = 10,
  removeFailsLimit = 10
) => {
  const actionFails = [];
  const removeFails = [];

  while (true) {
    const actionClicked = await clickActionMenuButton();
    if (actionClicked === false) {
      actionFails.push(1);
      if (actionFails.length >= actionFailsLimit) {
        console.error(
          `No more action menus after ${actionFailsLimit} attempts`
        );
        break;
      }
      await sleep(1000);
      continue;
    }

    actionFails.splice(0, actionFails.length);
    await sleep(1);

    const removeClicked = await clickRemoveButton(playlistName);
    if (removeClicked === false) {
      removeFails.push(1);
      if (removeFails.length >= removeFailsLimit) {
        console.error(
          `No remove button found after ${removeFailsLimit} attempts`
        );
        break;
      }
      await sleep(1000);
      continue;
    }

    removeFails.splice(0, removeFails.length);
    await sleep(1);
  }
};
