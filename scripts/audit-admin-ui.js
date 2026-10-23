// 后台 UI 视觉验证：截图 + 收集控制台错误 + 检查布局塌陷
const puppeteer = require('puppeteer-core');
const fs = require('fs');

const EDGE = '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const BASE = 'http://localhost:8090';
// 后台路径可配置，脚本按环境变量或默认 admin 拼接
const ADMIN = process.env.ADMIN_PATH || 'admin';
const OUT = '/Users/a123/Desktop/code/gocms/docs/screenshots-v2';

const pages = [
  ['a-login', '/login', false],
  ['a-dashboard', '', true],
  ['a-posts', '/posts', true],
  ['a-post-edit', '/posts/1/edit', true],
  ['a-categories', '/categories', true],
  ['a-category-edit', '/categories/1/edit', true],
  ['a-pages', '/pages', true],
  ['a-page-edit', '/pages/1/edit', true],
  ['a-tags', '/tags', true],
  ['a-media', '/media', true],
  ['a-menus', '/menus', true],
  ['a-menu-detail', '/menus/1', true],
  ['a-banners', '/banners', true],
  ['a-links', '/links', true],
  ['a-messages', '/messages', true],
  ['a-message-view', '/messages?view=1', true],
  ['a-users', '/users', true],
  ['a-settings', '/settings', true],
  ['a-audits', '/audits', true],
  ['a-settings-security', '/settings?group=security', true],
  ['a-theme', '/theme', true],
  ['a-profile', '/profile', true],
];

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  const browser = await puppeteer.launch({
    executablePath: EDGE,
    headless: 'new',
    args: ['--no-sandbox', '--disable-dev-shm-usage', '--hide-scrollbars'],
    defaultViewport: { width: 1440, height: 900 },
  });

  const page = await browser.newPage();
  const errors = [];
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push('[console] ' + m.text());
  });
  page.on('pageerror', (e) => errors.push('[pageerror] ' + e.message));
  page.on('requestfailed', (r) => errors.push('[reqfail] ' + r.url() + ' ' + r.failure()?.errorText));

  // 登录
  await page.goto(BASE + '/' + ADMIN + '/login', { waitUntil: 'networkidle0' });
  await page.type('#username', 'admin');
  await page.type('#password', 'Admin@123456');
  await Promise.all([
    page.waitForNavigation({ waitUntil: 'networkidle0' }),
    page.click('button[type=submit]'),
  ]);

  const report = [];
  for (const [name, path, needAuth] of pages) {
    const before = errors.length;
    await page.goto(BASE + '/' + ADMIN + path, { waitUntil: 'networkidle0' });

    // 布局审计：检测横向溢出与「被压成竖排」的操作列
    const audit = await page.evaluate(() => {
      const docW = document.documentElement.clientWidth;
      const overflow = document.documentElement.scrollWidth - docW;
      // 检查表格行高异常（竖排症状）
      const tallRows = [];
      document.querySelectorAll('table.data tbody tr').forEach((tr) => {
        const h = tr.getBoundingClientRect().height;
        if (h > 110) tallRows.push(Math.round(h));
      });
      // 检查是否有元素溢出视口右边界
      let outOfView = 0;
      document.querySelectorAll('main *').forEach((el) => {
        const r = el.getBoundingClientRect();
        if (r.width > 0 && r.right > docW + 2) outOfView++;
      });
      return { overflow, tallRows, outOfView };
    });

    const file = `${OUT}/${name}.png`;
    await page.screenshot({ path: file, fullPage: true });
    report.push({ name, path, ...audit, newErrors: errors.length - before });
  }

  // 深色模式：切一次主题再截 dashboard / posts
  await page.goto(BASE + '/' + ADMIN, { waitUntil: 'networkidle0' });
  await page.evaluate(() => {
    localStorage.setItem('admin-theme', 'dark');
    document.documentElement.classList.add('dark');
  });
  await page.reload({ waitUntil: 'networkidle0' });
  await page.screenshot({ path: `${OUT}/a-dashboard-dark.png`, fullPage: true });
  await page.goto(BASE + '/' + ADMIN + '/posts', { waitUntil: 'networkidle0' });
  await page.screenshot({ path: `${OUT}/a-posts-dark.png`, fullPage: true });
  await page.evaluate(() => localStorage.setItem('admin-theme', 'light'));

  // 移动端
  await page.setViewport({ width: 390, height: 844, isMobile: true, hasTouch: true });
  await page.goto(BASE + '/' + ADMIN, { waitUntil: 'networkidle0' });
  await page.screenshot({ path: `${OUT}/a-mobile-dashboard.png`, fullPage: true });

  await browser.close();

  console.log('=== 布局审计 ===');
  for (const r of report) {
    const flag = r.overflow > 2 || r.tallRows.length > 0 || r.newErrors > 0 ? '⚠️' : '✅';
    console.log(
      `${flag} ${r.name.padEnd(20)} 横向溢出=${String(r.overflow).padStart(3)}px  超高行=${JSON.stringify(r.tallRows)}  越界元素=${r.outOfView}  新错误=${r.newErrors}`
    );
  }
  console.log(`\n=== 控制台错误（${errors.length}）===`);
  [...new Set(errors)].slice(0, 25).forEach((e) => console.log('  ' + e));
})();
