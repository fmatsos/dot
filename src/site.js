// Small page-specific responses to the behaviours wired by base.js.
document.addEventListener("site:copied", event => {
  if (!event.detail.motion) return;
  const panel = event.target.closest(".secret-console, .quick-terminal");
  if (!panel) return;
  panel.classList.add("copied");
  setTimeout(() => panel.classList.remove("copied"), 900);
});
