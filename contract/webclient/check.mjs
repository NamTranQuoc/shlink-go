// Drives shlink-web-client (preconfigured with one server through SHLINK_SERVER_* env vars) through its main screens
// and fails if any API request the client makes gets an unexpected error response.
// Usage: CLIENT_URL=http://localhost:48090 SERVER_URL=http://localhost:48080 SERVER_NAME=Target node check.mjs
// (see scripts/webclient.sh, which starts both containers)
import { chromium } from 'playwright';

const CLIENT_URL = process.env.CLIENT_URL ?? 'http://localhost:48090';
const SERVER_URL = process.env.SERVER_URL ?? 'http://localhost:48080';
const SERVER_NAME = process.env.SERVER_NAME ?? 'Target';
const SLUG = `wc-${Date.now()}`;
// Without a Mercure hub, both upstream and the port answer mercure-info with 501, which the client handles.
const allowedErrors = [/\/mercure-info$/];

// BROWSER_CHANNEL=chrome uses an installed Google Chrome instead of Playwright's bundled Chromium.
const browser = await chromium.launch({ channel: process.env.BROWSER_CHANNEL || undefined });
const page = await browser.newPage();
const failures = [];
const apiCalls = [];
page.on('response', (res) => {
  const url = res.url();
  if (!url.startsWith(SERVER_URL)) return;
  apiCalls.push(`${res.request().method()} ${url.slice(SERVER_URL.length)} ${res.status()}`);
  if (res.status() >= 400 && !allowedErrors.some((re) => re.test(new URL(url).pathname))) {
    failures.push(`${res.request().method()} ${url} -> ${res.status()}`);
  }
});

const step = async (name, fn) => {
  process.stdout.write(`- ${name} ... `);
  await fn();
  console.log('ok');
};

try {
  let base;
  await step('open server', async () => {
    await page.goto(CLIENT_URL);
    await page.getByRole('link', { name: SERVER_NAME }).click();
    await page.getByRole('heading', { name: 'Create a short URL' }).waitFor();
    base = new URL(page.url()).pathname.replace(/\/overview$/, '');
  });
  await step('create short URL', async () => {
    await page.getByRole('textbox', { name: 'URL to be shortened' }).fill('https://example.com/web-client-check');
    await page.getByRole('textbox', { name: 'Custom slug' }).fill(SLUG);
    const tags = page.getByRole('combobox', { name: 'Add tags to the URL' });
    await tags.fill('webclient');
    await tags.press('Enter');
    await page.getByRole('button', { name: 'Save' }).click();
    await page.getByText('Great! The short URL is').waitFor();
  });
  await step('visit it', async () => {
    const res = await fetch(`${SERVER_URL}/${SLUG}`, { redirect: 'manual', headers: { 'User-Agent': 'Mozilla/5.0' } });
    if (res.status !== 302) throw new Error(`redirect returned ${res.status}`);
  });
  await step('list short URLs', async () => {
    await page.goto(`${CLIENT_URL}${base}/list-short-urls/1`);
    await page.getByRole('cell', { name: new RegExp(SLUG) }).first().waitFor();
  });
  await step('short URL visits', async () => {
    await page.goto(`${CLIENT_URL}${base}/short-code/${SLUG}/visits`);
    await page.getByText(/Visits for/i).first().waitFor();
    await page.getByText('Mozilla').first().waitFor({ state: 'attached' }).catch(() => {});
  });
  await step('edit short URL', async () => {
    await page.goto(`${CLIENT_URL}${base}/short-code/${SLUG}/edit`);
    const title = page.getByRole('textbox', { name: 'Title' });
    await title.fill('Edited from web client');
    const saved = page.waitForResponse((r) => r.request().method() === 'PATCH' && r.url().includes(SLUG));
    await page.getByRole('button', { name: 'Save' }).click();
    if ((await saved).status() !== 200) throw new Error('edit failed');
  });
  await step('redirect rules', async () => {
    await page.goto(`${CLIENT_URL}${base}/short-code/${SLUG}/redirect-rules`);
    await page.getByRole('button', { name: /Add rule/i }).first().waitFor();
  });
  await step('tags', async () => {
    await page.goto(`${CLIENT_URL}${base}/manage-tags`);
    await page.getByText('webclient').first().waitFor();
  });
  await step('domains', async () => {
    await page.goto(`${CLIENT_URL}${base}/manage-domains`);
    await page.getByText(new URL(SERVER_URL).host).first().waitFor();
  });
  await step('orphan and non-orphan visits', async () => {
    await page.goto(`${CLIENT_URL}${base}/orphan-visits`);
    await page.getByText(/Orphan visits/i).first().waitFor();
    await page.goto(`${CLIENT_URL}${base}/non-orphan-visits`);
    await page.getByText(/Non-orphan visits/i).first().waitFor();
  });
  await step('delete short URL', async () => {
    await page.goto(`${CLIENT_URL}${base}/list-short-urls/1`);
    const row = page.getByRole('row').filter({ hasText: SLUG });
    await row.getByRole('button', { name: 'Options' }).click();
    await page.getByRole('menuitem', { name: /Delete short URL/i }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByRole('textbox').fill('delete');
    await dialog.getByRole('button', { name: /^Delete$/ }).click();
    await row.waitFor({ state: 'detached' });
  });
} catch (e) {
  failures.push(`step failed: ${e.message.split('\n')[0]}`);
} finally {
  await browser.close();
}

console.log(`\n${apiCalls.length} API calls made:\n  ${[...new Set(apiCalls)].join('\n  ')}`);
if (failures.length) {
  console.error(`\nFAILED:\n  ${failures.join('\n  ')}`);
  process.exit(1);
}
console.log('\nweb client check passed');
