// Apply the saved theme before first paint (no flash). A separate file, not
// an inline script, so the Content-Security-Policy can forbid inline scripts.
try {
  var t = localStorage.getItem("theme");
  if (t === "dark" || (!t && matchMedia("(prefers-color-scheme: dark)").matches))
    document.documentElement.classList.add("dark");
} catch (e) {}
