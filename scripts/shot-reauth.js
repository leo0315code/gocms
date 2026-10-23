const puppeteer = require('puppeteer-core');
const BASE = 'http://localhost:8090';
const ADMIN = process.env.ADMIN_PATH || 'admin';
const BASE = 'http://localhost:8090';
const ADMIN = process.env.ADMIN_PATH || 'admin';
(async () => {
  const b = await puppeteer.launch({
    executablePath: '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
    headless: 'new', args: ['--no-sandbox','--hide-scrollbars'],
    defaultViewport: { width: 1440, height: 900 },
  });
  const p = await b.newPage();
  await p.goto(BASE + '/' + ADMIN + '/login', { waitUntil: 'networkidle0' });
  await p.type('#username','admin'); await p.type('#password','Admin@123456');
  await Promise.all([p.waitForNavigation({waitUntil:'networkidle0'}), p.click('button[type=submit]')]);
  await p.goto(BASE + '/' + ADMIN + '/settings?group=security', { waitUntil:'networkidle0' });
  // 修改后台路径输入框，触发二次验证区展开
  await p.click('#setting_admin_path', { clickCount: 3 });
  await p.type('#setting_admin_path', 'vault-demo');
  await new Promise(r => setTimeout(r, 500));
  await p.screenshot({ path: 'docs/screenshots-v2/a-settings-reauth.png', fullPage: true });
  console.log('saved');
  await b.close();
})();
