/**
 * Slack token extraction via Playwright.
 * Usage: node auth.js <workspace-url> <profile-dir>
 *
 * Writes JSON to stdout on success:
 *   { token, cookies, cookie_header, timestamp, workspace }
 * Writes progress JSON lines to stderr:
 *   { step, message }
 * Exits 1 on failure.
 */

import { chromium } from 'playwright';

const [, , workspaceURL, profileDir] = process.argv;

if (!workspaceURL || !profileDir) {
  process.stderr.write(JSON.stringify({ step: 'error', message: 'Usage: node auth.js <workspace-url> <profile-dir>' }) + '\n');
  process.exit(1);
}

const log = (step, message) => {
  process.stderr.write(JSON.stringify({ step, message }) + '\n');
};

let slackApiToken = null;

log('launching', 'Opening browser...');

const context = await chromium.launchPersistentContext(profileDir, {
  headless: false,
  viewport: { width: 1280, height: 720 },
  args: ['--no-sandbox'],
});

const page = await context.newPage();

// Intercept requests to capture the API token from multipart form body
page.on('request', (request) => {
  const url = request.url();

  if (slackApiToken || !url.includes('slack.com/api/')) {
    return;
  }

  const method = url.split('/api/')[1]?.split('?')[0];

  if (
    (method === 'conversations.history' || method === 'conversations.replies') &&
    request.method() === 'POST'
  ) {
    try {
      const postData = request.postData();

      if (postData) {
        const match = postData.match(/name="token"[\r\n]+[\r\n]+([^\r\n\s]+)/);

        if (match) {
          slackApiToken = match[1].trim();
          log('token-captured', `Captured token: ${slackApiToken.substring(0, 15)}...`);
        }
      }
    } catch (_e) {
      // ignore interception errors
    }
  }
});

try {
  log('loading', 'Opening Slack workspace...');
  await page.goto(workspaceURL);

  log('waiting', 'Waiting for workspace to load (complete SSO if prompted)...');
  await page.waitForSelector('[role="tree"]', { timeout: 120000 });

  log('loaded', 'Workspace loaded');
  log('triggering', 'Triggering API call to capture token...');

  const modifier = process.platform === 'darwin' ? 'Meta' : 'Control';

  await page.keyboard.press(`${modifier}+k`);
  await page.waitForTimeout(500);
  await page.keyboard.type('general');
  await page.waitForTimeout(1000);
  await page.keyboard.press('Enter');
  await page.waitForTimeout(3000);

  if (!slackApiToken) {
    log('waiting-token', 'Waiting for API token...');

    for (let i = 0; i < 20 && !slackApiToken; i++) {
      await page.waitForTimeout(500);
    }
  }

  if (!slackApiToken) {
    throw new Error('Failed to capture API token. Try manually navigating to a channel.');
  }

  log('cookies', 'Capturing cookies...');

  const cookies = await context.cookies();
  const cookieHeader = cookies.map((c) => `${c.name}=${c.value}`).join('; ');
  const workspace = new URL(workspaceURL).hostname.split('.')[0];

  const tokenData = {
    token: slackApiToken,
    cookies: cookies,
    cookie_header: cookieHeader,
    timestamp: new Date().toISOString(),
    workspace: workspace,
  };

  process.stdout.write(JSON.stringify(tokenData, null, 2) + '\n');

  log('complete', 'Authentication successful');
} catch (error) {
  log('error', `Error: ${error.message}`);
  process.exit(1);
} finally {
  await context.close();
}
