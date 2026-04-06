/**
 * ServiceNow token extraction via Playwright.
 * Usage: node auth.js <instance-url> <profile-dir>
 *
 * Writes JSON to stdout on success:
 *   { g_ck, cookies, cookie_header, instance, timestamp }
 * Writes progress JSON lines to stderr:
 *   { step, message }
 * Exits 1 on failure.
 */

import { chromium } from 'playwright';

const [, , instanceURL, profileDir] = process.argv;

if (!instanceURL || !profileDir) {
  process.stderr.write(JSON.stringify({ step: 'error', message: 'Usage: node auth.js <instance-url> <profile-dir>' }) + '\n');
  process.exit(1);
}

const log = (step, message) => {
  process.stderr.write(JSON.stringify({ step, message }) + '\n');
};

log('launching', 'Opening browser...');

const context = await chromium.launchPersistentContext(profileDir, {
  headless: false,
  viewport: { width: 1280, height: 720 },
  args: ['--no-sandbox'],
});

const page = await context.newPage();

try {
  // Navigate to a ServiceNow page that triggers SSO and sets g_ck
  const loginURL = instanceURL.replace(/\/$/, '') + '/now/nav/ui/classic/params/target/incident_list.do';
  log('loading', `Opening ServiceNow at ${instanceURL}...`);
  await page.goto(loginURL);

  log('waiting', 'Waiting for SSO login to complete...');

  // Wait for g_ck to be set — this only happens after successful SSO authentication
  await page.waitForFunction(() => {
    return typeof window.g_ck === 'string' && window.g_ck.length > 10;
  }, { timeout: 120000 });

  log('authenticated', 'SSO authentication detected');
  log('extracting', 'Extracting session token...');

  const gck = await page.evaluate(() => window.g_ck);

  log('cookies', 'Capturing cookies...');

  const cookies = await context.cookies();
  const cookieHeader = cookies.map((c) => `${c.name}=${c.value}`).join('; ');

  const instance = new URL(instanceURL).hostname.split('.')[0];

  const tokenData = {
    g_ck: gck,
    cookies: cookies,
    cookie_header: cookieHeader,
    instance: instance,
    timestamp: new Date().toISOString(),
  };

  process.stdout.write(JSON.stringify(tokenData, null, 2) + '\n');

  log('complete', 'Authentication successful');
} catch (error) {
  log('error', `Error: ${error.message}`);
  process.exit(1);
} finally {
  await context.close();
}
