/* GoCMS 默认主题交互 */
(function () {
  'use strict';

  /* ---------- 主题切换 ---------- */
  var root = document.documentElement;
  var toggle = document.getElementById('themeToggle');
  if (toggle) {
    toggle.addEventListener('click', function () {
      var next = root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
      root.setAttribute('data-theme', next);
      try { localStorage.setItem('theme', next); } catch (e) {}
    });
  }

  /* ---------- 移动端菜单 ---------- */
  var menuBtn = document.getElementById('mobileMenuBtn');
  var nav = document.getElementById('mainNav');
  if (menuBtn && nav) {
    menuBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      nav.classList.toggle('open');
    });
    document.addEventListener('click', function (e) {
      if (nav.classList.contains('open') && !nav.contains(e.target) && e.target !== menuBtn) {
        nav.classList.remove('open');
      }
    });
  }

  /* ---------- 首页轮播 ---------- */
  var slides = document.querySelectorAll('.hero-slide');
  var dots = document.querySelectorAll('.hero-dot');
  if (slides.length > 1) {
    var cur = 0;
    var timer = null;

    function show(i) {
      slides.forEach(function (s, idx) { s.classList.toggle('active', idx === i); });
      dots.forEach(function (d, idx) { d.classList.toggle('active', idx === i); });
      cur = i;
    }

    function next() { show((cur + 1) % slides.length); }

    function start() {
      stop();
      timer = setInterval(next, 6000);
    }
    function stop() {
      if (timer) { clearInterval(timer); timer = null; }
    }

    dots.forEach(function (d) {
      d.addEventListener('click', function () {
        show(parseInt(d.getAttribute('data-index'), 10) || 0);
        start();
      });
    });

    var hero = document.querySelector('.hero');
    if (hero) {
      hero.addEventListener('mouseenter', stop);
      hero.addEventListener('mouseleave', start);
    }

    if (!window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      start();
    }
  }

  /* ---------- 正文外链新窗口 + nofollow ---------- */
  var content = document.querySelector('.prose');
  if (content) {
    content.querySelectorAll('a[href^="http"]').forEach(function (a) {
      if (a.hostname && a.hostname !== location.hostname) {
        a.setAttribute('target', '_blank');
        a.setAttribute('rel', 'noopener nofollow');
      }
    });
  }

  /* ---------- 代码块复制按钮 ---------- */
  document.querySelectorAll('.prose pre').forEach(function (pre) {
    var btn = document.createElement('button');
    btn.className = 'code-copy';
    btn.type = 'button';
    btn.textContent = '复制';
    btn.addEventListener('click', function () {
      var code = pre.querySelector('code');
      var text = code ? code.innerText : pre.innerText;
      if (navigator.clipboard) {
        navigator.clipboard.writeText(text).then(function () {
          btn.textContent = '已复制';
          setTimeout(function () { btn.textContent = '复制'; }, 1600);
        });
      }
    });
    pre.style.position = 'relative';
    pre.appendChild(btn);
  });

  /* ---------- 表单防重复提交 ---------- */
  document.querySelectorAll('form[method="post"]').forEach(function (form) {
    form.addEventListener('submit', function () {
      var btn = form.querySelector('button[type="submit"]');
      if (btn && !btn.disabled) {
        btn.disabled = true;
        var txt = btn.textContent;
        btn.textContent = '处理中…';
        setTimeout(function () { btn.disabled = false; btn.textContent = txt; }, 8000);
      }
    });
  });
})();
