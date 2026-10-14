(function () {
  var RELEASE = "https://github.com/auth-com-br/tend/releases/latest/download/";

  bindCopy();
  bindInstall();
  loadGitHubStars();

  function loadGitHubStars() {
    var badges = document.querySelectorAll("[data-star-count]");
    if (!badges.length || !window.fetch) return;
    var key = "tend-github-stars-v1";
    var ttl = 60 * 60 * 1000;

    function valid(count) {
      return typeof count === "number" && isFinite(count) && count >= 0 && Math.floor(count) === count;
    }

    function show(count) {
      var label = String(count);
      try { label = count.toLocaleString(document.documentElement.lang); } catch (e) {}
      for (var i = 0; i < badges.length; i++) {
        badges[i].querySelector("[data-star-value]").textContent = label;
        badges[i].querySelector("[data-star-unit]").textContent = badges[i].getAttribute(count === 1 ? "data-star-singular" : "data-star-plural");
        badges[i].setAttribute("aria-label", badges[i].getAttribute("data-stars-label") + ": " + label);
        badges[i].hidden = false;
      }
    }

    // One public request per browser per hour keeps language switches from
    // spending GitHub's unauthenticated quota. Never invent a fallback count.
    try {
      var saved = JSON.parse(localStorage.getItem(key));
      var age = saved && Date.now() - saved.at;
      if (saved && valid(saved.count) && typeof saved.at === "number" && age >= 0 && age < ttl) {
        show(saved.count);
        return;
      }
    } catch (e) {}

    window.fetch("https://api.github.com/repos/auth-com-br/tend", {
      headers: { "Accept": "application/vnd.github+json" },
      credentials: "omit",
      referrerPolicy: "no-referrer"
    }).then(function (response) {
      if (!response.ok) throw new Error("GitHub count unavailable");
      return response.json();
    }).then(function (repo) {
      if (!repo || !valid(repo.stargazers_count)) return;
      show(repo.stargazers_count);
      try {
        localStorage.setItem(key, JSON.stringify({count: repo.stargazers_count, at: Date.now()}));
      } catch (e) {}
    }).catch(function () {
      // The link and invitation remain usable when offline or rate-limited.
    });
  }

  function bindCopy() {
    var buttons = document.querySelectorAll(".copy[data-copy]");
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].addEventListener("click", onCopy);
    }
  }

  function onCopy(ev) {
    var btn = ev.currentTarget;
    var text = btn.getAttribute("data-copy") || "";
    var done = btn.getAttribute("data-done") || "copied";
    var label = btn.getAttribute("data-label") || btn.textContent;

    function ok() {
      btn.textContent = done;
      btn.classList.add("is-done");
      window.setTimeout(function () {
        btn.textContent = label;
        btn.classList.remove("is-done");
      }, 1600);
    }

    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(ok).catch(function () {
        fallback(text, ok);
      });
      return;
    }
    fallback(text, ok);
  }

  function fallback(text, ok) {
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.left = "-9999px";
    document.body.appendChild(ta);
    ta.select();
    try {
      document.execCommand("copy");
      ok();
    } finally {
      document.body.removeChild(ta);
    }
  }

  function bindInstall() {
    var root = document.getElementById("install");
    if (!root) return;

    var tabs = root.querySelectorAll(".os-tabs [data-os]");
    var detected = detectOS();
    var arch = detectArch();
    var selected = detected;

    function paint() {
      applyOS(root, selected, arch, detected);
    }

    for (var i = 0; i < tabs.length; i++) {
      tabs[i].addEventListener("click", function () {
        selected = this.getAttribute("data-os") || selected;
        paint();
      });
    }

    paint();

    // Chromium can tell arm vs x86; Safari still reports MacIntel on Apple silicon.
    if (navigator.userAgentData && navigator.userAgentData.getHighEntropyValues) {
      navigator.userAgentData.getHighEntropyValues(["architecture"]).then(function (info) {
        var next = archFromHint(info && info.architecture);
        if (!next || next === arch) return;
        arch = next;
        paint();
      }).catch(function () {});
    }
  }

  function detectOS() {
    var ua = navigator.userAgent || "";
    var plat = "";
    if (navigator.userAgentData && navigator.userAgentData.platform) {
      plat = navigator.userAgentData.platform;
    } else if (navigator.platform) {
      plat = navigator.platform;
    }
    var hay = (plat + " " + ua).toLowerCase();
    if (/win/.test(hay)) return "windows";
    if (/mac|iphone|ipad|ipod/.test(hay)) return "darwin";
    if (/linux|cros|android/.test(hay)) return "linux";
    return "linux";
  }

  function detectArch() {
    var ua = navigator.userAgent || "";
    if (/aarch64|arm64|armv8/i.test(ua)) return "arm64";
    return "amd64";
  }

  function archFromHint(value) {
    var a = String(value || "").toLowerCase();
    if (a === "arm" || a === "arm64") return "arm64";
    if (a === "x86" || a === "x86_64") return "amd64";
    return "";
  }

  function applyOS(root, os, arch, detected) {
    var tabs = root.querySelectorAll(".os-tabs [data-os]");
    for (var i = 0; i < tabs.length; i++) {
      var on = tabs[i].getAttribute("data-os") === os;
      tabs[i].setAttribute("aria-selected", on ? "true" : "false");
    }

    var unix = root.querySelector('[data-panel="unix"]');
    var win = root.querySelector('[data-panel="windows"]');
    var isWin = os === "windows";
    if (unix) unix.hidden = isWin;
    if (win) win.hidden = !isWin;

    var hint = document.getElementById("os-detected");
    if (hint) {
      var key = "detected-linux";
      if (detected === "darwin") key = "detected-macos";
      if (detected === "windows") key = "detected-windows";
      var label = root.getAttribute("data-" + key) || "";
      hint.textContent = label;
      hint.hidden = !label;
    }

    if (isWin) return;

    var goos = os === "darwin" ? "darwin" : "linux";
    var other = arch === "arm64" ? "amd64" : "arm64";
    var primary = "tend-" + goos + "-" + arch;
    var secondary = "tend-" + goos + "-" + other;
    var prefix = root.getAttribute("data-download") || "Download";

    var bin = document.getElementById("bin-link");
    if (bin) {
      bin.href = RELEASE + primary;
      bin.textContent = prefix + " " + primary;
    }
    var alt = document.getElementById("bin-alt");
    if (alt) {
      alt.href = RELEASE + secondary;
      alt.textContent = other;
    }
  }
})();
