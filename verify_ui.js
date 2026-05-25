const { chromium } = require('playwright');
const path = require('path');

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();

  // Start the dev server in the background
  const { spawn } = require('child_process');
  const server = spawn('npm', ['run', 'dev'], {
    env: { ...process.env, PORT: '3000' }
  });

  server.stdout.on('data', (data) => {
    console.log(`stdout: ${data}`);
  });

  // Wait for server to be ready
  await new Promise(resolve => setTimeout(resolve, 10000));

  try {
    // Check Home (Matches)
    await page.goto('http://localhost:3000');
    await page.screenshot({ path: 'screenshot-matches.png' });
    console.log('Matches page screenshot saved.');

    // Check Leagues
    await page.goto('http://localhost:3000/leagues');
    await page.screenshot({ path: 'screenshot-leagues.png' });
    console.log('Leagues page screenshot saved.');

    // Check News
    await page.goto('http://localhost:3000/news');
    await page.screenshot({ path: 'screenshot-news.png' });
    console.log('News page screenshot saved.');

  } catch (err) {
    console.error('UI verification failed:', err);
  } finally {
    server.kill();
    await browser.close();
  }
})();
