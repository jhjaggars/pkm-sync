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

// SSO (e.g. IdP + MFA) can take a while; give the user plenty of time.
const LOGIN_TIMEOUT_MS = 5 * 60 * 1000;
const POLL_INTERVAL_MS = 1000;

const workspaceHost = new URL(workspaceURL).hostname;
const workspace = workspaceHost.split('.')[0];

let slackApiToken = null;
let contextClosed = false;

log('launching', 'Opening browser...');

const context = await chromium.launchPersistentContext(profileDir, {
  headless: false,
  viewport: { width: 1280, height: 720 },
  args: ['--no-sandbox'],
});

context.on('close', () => {
  contextClosed = true;
});

// Capture the xoxc token from any Slack API call made by any tab in the context.
// SSO flows often replace or close the original tab, so a page-level listener is not enough.
context.on('request', (request) => {
  if (slackApiToken || request.method() !== 'POST' || !request.url().includes('slack.com/api/')) {
    return;
  }

  try {
    const postData = request.postData();

    if (!postData) {
      return;
    }

    const match =
      postData.match(/name="token"[\r\n]+[\r\n]+(xoxc-[^\r\n\s]+)/) || postData.match(/(?:^|&)token=(xoxc-[^&\s]+)/);

    if (match) {
      slackApiToken = decodeURIComponent(match[1].trim());
      log('token-captured', `Captured token: ${slackApiToken.substring(0, 15)}...`);
    }
  } catch (_e) {
    // ignore interception errors
  }
});

const isSlackRedirectPage = (url) => /\/ssb\/redirect|\/signin\/.*redirect|slack:\/\//.test(url);

// readTokenFromStorage reads the xoxc token Slack's web client keeps in localStorage.
const readTokenFromStorage = async (page) => {
  try {
    return await page.evaluate((host) => {
      const raw = localStorage.getItem('localConfig_v2');

      if (!raw) {
        return null;
      }

      const teams = Object.values(JSON.parse(raw).teams || {});
      const team = teams.find((t) => t.url && new URL(t.url).hostname === host) || teams[0];

      return team?.token || null;
    }, workspaceHost);
  } catch (_e) {
    return null;
  }
};

// findClientPage returns an open tab showing the loaded Slack client, recovering from
// redirect/landing pages and closed tabs along the way.
const findClientPage = async () => {
  const deadline = Date.now() + LOGIN_TIMEOUT_MS;

  while (Date.now() < deadline) {
    if (contextClosed) {
      throw new Error('Browser was closed before Slack finished loading. Leave the window open until auth completes.');
    }

    const pages = context.pages().filter((p) => !p.isClosed());

    if (pages.length === 0) {
      log('reopening', 'Slack tab was closed; reopening workspace...');
      await context.newPage().then((p) => p.goto(workspaceURL)).catch(() => {});

      continue;
    }

    for (const p of pages) {
      const url = p.url();

      // After SSO Slack may land on an "open in desktop app" page; send it to the web client instead.
      if (isSlackRedirectPage(url)) {
        log('redirecting', 'Skipping desktop-app redirect; opening Slack in the browser...');
        await p.goto(workspaceURL).catch(() => {});

        continue;
      }

      const loaded = await p
        .locator('[role="tree"]')
        .first()
        .isVisible()
        .catch(() => false);

      if (loaded) {
        return p;
      }

      if (!slackApiToken && url.includes('app.slack.com/client')) {
        slackApiToken = await readTokenFromStorage(p);

        if (slackApiToken) {
          log('token-captured', `Read token from browser storage: ${slackApiToken.substring(0, 15)}...`);

          return p;
        }
      }
    }

    if (slackApiToken) {
      return pages.find((p) => p.url().includes('slack.com')) || pages[0];
    }

    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }

  throw new Error(`Timed out after ${LOGIN_TIMEOUT_MS / 60000} minutes waiting for the Slack workspace to load.`);
};

try {
  log('loading', 'Opening Slack workspace...');

  // Reuse the tab a persistent context opens by default instead of leaving a stray blank tab.
  const initialPage = context.pages()[0] || (await context.newPage());

  await initialPage.goto(workspaceURL);

  log('waiting', 'Waiting for workspace to load (complete SSO if prompted)...');

  const page = await findClientPage();

  log('loaded', 'Workspace loaded');

  if (!slackApiToken) {
    slackApiToken = await readTokenFromStorage(page);

    if (slackApiToken) {
      log('token-captured', `Read token from browser storage: ${slackApiToken.substring(0, 15)}...`);
    }
  }

  if (!slackApiToken) {
    log('triggering', 'Triggering API call to capture token...');

    const modifier = process.platform === 'darwin' ? 'Meta' : 'Control';

    await page.keyboard.press(`${modifier}+k`);
    await page.waitForTimeout(500);
    await page.keyboard.type('general');
    await page.waitForTimeout(1000);
    await page.keyboard.press('Enter');
    await page.waitForTimeout(3000);
  }

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
  process.exitCode = 1;
} finally {
  if (!contextClosed) {
    await context.close().catch(() => {});
  }
}
