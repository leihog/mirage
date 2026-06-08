(() => {
  const key = "mirage-theme";
  const media = matchMedia("(prefers-color-scheme: dark)");
  const stored = localStorage.getItem(key);
  const choice = stored === "light" || stored === "dark" || stored === "auto" ? stored : "auto";
  document.documentElement.dataset.themeChoice = choice;
  document.documentElement.dataset.theme = choice === "auto" ? (media.matches ? "dark" : "light") : choice;
})();
