const puppeteer = require('puppeteer-core');
(async () => {
  const b = await puppeteer.launch({
    executablePath: '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
    headless: 'new', args: ['--no-sandbox'], defaultViewport: {width:1440,height:900},
  });
  const p = await b.newPage();
  await p.goto('http://localhost:8090/manage/login', { waitUntil:'networkidle0' });
  await p.type('#username','admin'); await p.type('#password','Admin@123456');
  await Promise.all([p.waitForNavigation({waitUntil:'networkidle0'}), p.click('button[type=submit]')]);
  await p.goto('http://localhost:8090/manage/media', { waitUntil:'networkidle0' });

  const r = await p.evaluate(async () => {
    const base = document.body.getAttribute('data-base-path');
    const token = document.querySelector('input[name="csrf_token"]');
    // 用 GET 探测上传端点是否存在（POST 不带文件会返回 400/405，但绝不会是 404）
    const res = await fetch(base + '/upload.json', { method: 'POST', headers: { 'X-CSRF-Token': token ? token.value : '' } });
    return { base, status: res.status, appJsLoaded: !!document.querySelector('script[src$="/assets/app.js"]') };
  });
  console.log(JSON.stringify(r));
  await b.close();
})();
