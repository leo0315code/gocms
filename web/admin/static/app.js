/* GoCMS 后台交互
 *
 * 分工：
 *   - 导航开合、下拉菜单、主题切换、模态框等 UI 状态 → 交给 Alpine（模板里 x-data）
 *   - 上传、富文本编辑、剪贴板、确认提交等功能性逻辑 → 留在本文件
 */
(function () {
  'use strict';

  /* ---------- 上传 ---------- */

  window.uploadFile = function (file, cb) {
    var fd = new FormData();
    fd.append('file', file);
    var token = document.querySelector('input[name="csrf_token"]');
    // 后台路径可配置，从 <body data-base-path> 读取。
    // 不做 '/admin' 兜底：属性缺失说明模板异常，猜一个默认值只会在改过路径后
    // 把上传请求打到 404 上，反而更难排查。
    var base = document.body.getAttribute('data-base-path');
    if (!base) {
      alert('页面缺少 data-base-path 属性，无法上传。请刷新页面或检查模板。');
      return;
    }
    var xhr = new XMLHttpRequest();
    xhr.open('POST', base + '/upload.json');
    xhr.setRequestHeader('X-CSRF-Token', token ? token.value : '');
    xhr.onload = function () {
      try {
        var res = JSON.parse(xhr.responseText);
        if (res.url) { cb(res.url); }
        else { alert(res.error || '上传失败'); }
      } catch (e) { alert('上传失败：响应解析错误'); }
    };
    xhr.onerror = function () { alert('上传失败：网络错误'); };
    xhr.send(fd);
  };

  window.pickImage = function (inputId, previewId) {
    var input = document.getElementById(inputId);
    var fileInput = document.createElement('input');
    fileInput.type = 'file';
    fileInput.accept = 'image/*';
    fileInput.onchange = function () {
      if (!fileInput.files.length) return;
      uploadFile(fileInput.files[0], function (url) {
        input.value = url;
        var pv = document.getElementById(previewId);
        if (pv) pv.innerHTML = '<img src="' + url + '" alt="" class="w-full h-full object-cover">';
      });
    };
    fileInput.click();
  };

  /* ---------- 拖拽上传 ---------- */
  document.querySelectorAll('[data-dropzone]').forEach(function (zone) {
    var input = zone.querySelector('input[type="file"]');
    if (!input) return;

    zone.addEventListener('click', function (e) {
      if (e.target !== input) input.click();
    });
    ['dragenter', 'dragover'].forEach(function (ev) {
      zone.addEventListener(ev, function (e) {
        e.preventDefault();
        zone.classList.add('border-brand-500', 'bg-brand-50', 'dark:bg-brand-500/10');
      });
    });
    ['dragleave', 'drop'].forEach(function (ev) {
      zone.addEventListener(ev, function (e) {
        e.preventDefault();
        zone.classList.remove('border-brand-500', 'bg-brand-50', 'dark:bg-brand-500/10');
      });
    });
    zone.addEventListener('drop', function (e) {
      var dt = e.dataTransfer;
      if (!dt || !dt.files.length) return;
      if (zone.dataset.auto === '1') {
        var form = document.getElementById('uploadForm');
        if (form) {
          form.querySelector('input[type="file"]').files = dt.files;
          form.submit();
        }
      }
    });
    input.addEventListener('change', function () {
      if (zone.dataset.auto === '1' && input.files.length) {
        var form = document.getElementById('uploadForm');
        if (form) form.submit();
      }
    });
  });

  /* ---------- 富文本编辑器 ---------- */
  var area = document.getElementById('editorArea');
  if (area) {
    function syncEditor() {
      var hidden = document.getElementById('postContent');
      if (hidden) hidden.value = area.innerHTML;
    }

    document.querySelectorAll('.etool[data-cmd]').forEach(function (btn) {
      btn.addEventListener('click', function (e) {
        e.preventDefault();
        var cmd = btn.dataset.cmd;
        var val = btn.dataset.val || null;
        if (cmd === 'createLink') {
          var url = prompt('请输入链接地址', 'https://');
          if (!url) return;
          document.execCommand('createLink', false, url);
        } else if (cmd === 'insertImage') {
          var picker = document.createElement('input');
          picker.type = 'file';
          picker.accept = 'image/*';
          picker.onchange = function () {
            if (!picker.files.length) return;
            uploadFile(picker.files[0], function (u) {
              area.focus();
              document.execCommand('insertImage', false, u);
            });
          };
          picker.click();
        } else if (cmd === 'formatBlock') {
          document.execCommand('formatBlock', false, val);
        } else {
          document.execCommand(cmd, false, val);
        }
        area.focus();
        syncEditor();
      });
    });

    area.addEventListener('input', syncEditor);
    area.addEventListener('blur', syncEditor);

    var form = area.closest('form');
    if (form) form.addEventListener('submit', syncEditor);

    // 粘贴图片自动上传
    area.addEventListener('paste', function (e) {
      var items = (e.clipboardData || {}).items || [];
      for (var i = 0; i < items.length; i++) {
        if (items[i].type.indexOf('image') === 0) {
          e.preventDefault();
          var f = items[i].getAsFile();
          uploadFile(f, function (u) {
            area.focus();
            document.execCommand('insertImage', false, u);
          });
          return;
        }
      }
    });
  }

  /* ---------- 编辑器 Markdown / 富文本 切换 ---------- */
  var tabs = document.querySelectorAll('.editor-tab');
  if (tabs.length) {
    tabs.forEach(function (tab) {
      tab.addEventListener('click', function () {
        var mode = tab.dataset.mode;
        var fmt = document.getElementById('contentFormat');
        if (fmt) fmt.value = mode;
        tabs.forEach(function (t) {
          var on = t === tab;
          t.classList.toggle('bg-surface', on);
          t.classList.toggle('text-brand-700', on);
          t.classList.toggle('dark:text-brand-300', on);
          t.classList.toggle('text-ink-3', !on);
        });

        var rich = document.getElementById('richWrap');
        var mdWrap = document.getElementById('mdWrap');
        var hidden = document.getElementById('postContent');
        if (!rich || !mdWrap || !hidden) return;

        if (mode === 'markdown') {
          if (area) mdWrap.querySelector('textarea').value = htmlToText(area.innerHTML);
          rich.classList.add('hidden');
          mdWrap.classList.remove('hidden');
        } else {
          rich.classList.remove('hidden');
          mdWrap.classList.add('hidden');
          if (area) area.innerHTML = hidden.value;
        }
      });
    });

    var mdArea = document.getElementById('mdArea');
    if (mdArea) {
      mdArea.addEventListener('input', function () {
        var hidden = document.getElementById('postContent');
        if (hidden) hidden.value = mdArea.value;
      });
      var form2 = mdArea.closest('form');
      if (form2) form2.addEventListener('submit', function () {
        var hidden = document.getElementById('postContent');
        var fmt = document.getElementById('contentFormat');
        if (hidden && fmt && fmt.value === 'markdown') {
          hidden.value = mdArea.value;
        }
      });
    }
  }

  function htmlToText(html) {
    return html
      .replace(/<br\s*\/?>/gi, '\n')
      .replace(/<\/p>/gi, '\n\n')
      .replace(/<\/h[1-6]>/gi, '\n\n')
      .replace(/<li>/gi, '- ')
      .replace(/<\/li>/gi, '\n')
      .replace(/<[^>]+>/g, '')
      .replace(/&nbsp;/g, ' ')
      .replace(/&amp;/g, '&')
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .trim();
  }

  /* ---------- 复制到剪贴板 ---------- */
  window.copyText = function (text, btn) {
    if (!navigator.clipboard) return;
    navigator.clipboard.writeText(text).then(function () {
      if (!btn) return;
      var t = btn.textContent;
      btn.textContent = '已复制';
      setTimeout(function () { btn.textContent = t; }, 1400);
    });
  };

  /* ---------- 提交防重复 ---------- */
  document.addEventListener('submit', function (e) {
    var form = e.target;
    var msg = form.getAttribute('data-confirm');
    if (msg && !window.confirm(msg)) {
      e.preventDefault();
      return;
    }
    var btn = form.querySelector('button[type="submit"]');
    if (btn && !btn.dataset.nolock && !btn.disabled) {
      btn.dataset.origText = btn.textContent;
      btn.disabled = true;
      btn.textContent = '处理中…';
    }
  }, true);

  /* ---------- flash 自动淡出 ---------- */
  var flash = document.querySelector('.flash');
  if (flash) {
    setTimeout(function () {
      flash.style.transition = 'opacity .4s, max-height .4s, margin .4s, padding .4s';
      flash.style.overflow = 'hidden';
      flash.style.maxHeight = flash.offsetHeight + 'px';
      requestAnimationFrame(function () {
        flash.style.opacity = '0';
        flash.style.maxHeight = '0';
        flash.style.paddingTop = '0';
        flash.style.paddingBottom = '0';
        flash.style.marginTop = '0';
      });
    }, 4200);
  }
})();
