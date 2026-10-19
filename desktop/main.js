// Handoff desktop: a thin shell around a hosted workspace, like Slack's app.
const { app, BrowserWindow, Menu, ipcMain, shell } = require('electron');
const fs = require('fs');
const path = require('path');

const PARTITION = 'persist:handoff';
const isDev = !app.isPackaged;

// Sign-in flows that legitimately leave the workspace host and come back.
// Google bounces through accounts.google.com and the server's apex host.
const AUTH_HOSTS = ['accounts.google.com'];

const configPath = () => path.join(app.getPath('userData'), 'config.json');
const readConfig = () => {
  try {
    return JSON.parse(fs.readFileSync(configPath(), 'utf8'));
  } catch {
    return {};
  }
};
const writeConfig = (cfg) => fs.writeFileSync(configPath(), JSON.stringify(cfg, null, 2));

let win = null;
let workspace = null; // URL origin of the workspace, e.g. https://acme.example.com

// Hosts the window may navigate to: the workspace itself, its parent domain
// (the apex hosts signup and the shared auth callback), and auth providers.
const allowedInApp = (link) => {
  let u;
  try {
    u = new URL(link);
  } catch {
    return false;
  }
  if (u.protocol !== 'https:' && u.protocol !== 'http:') return false;
  if (!workspace) return false;
  if (u.host === new URL(workspace).host) return true; // exact host, port included
  const apex = new URL(workspace).hostname.split('.').slice(1).join('.');
  if (apex && (u.hostname === apex || u.hostname.endsWith('.' + apex))) return true;
  return AUTH_HOSTS.includes(u.hostname);
};

// Everything else opens in the system browser; inert schemes (blob:, data:, javascript:) die quietly.
const openOutside = (link) => {
  if (/^(https?|mailto|tel):/i.test(link)) shell.openExternal(link);
};
const guardNavigation = (event, link, isMainFrame = true) => {
  if (!isMainFrame || allowedInApp(link)) return;
  event.preventDefault();
  openOutside(link);
};

function createWindow() {
  win = new BrowserWindow({
    title: 'Handoff',
    width: 1200,
    height: 800,
    minWidth: 1000,
    minHeight: 700,
    webPreferences: {
      partition: PARTITION,
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      spellcheck: true,
    },
  });
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (allowedInApp(url)) win.loadURL(url);
    else openOutside(url);
    return { action: 'deny' };
  });
  win.webContents.on('will-navigate', (e, url) => guardNavigation(e, url));
  win.webContents.on('will-redirect', (e, url, _inPlace, isMainFrame) => guardNavigation(e, url, isMainFrame));
  workspace ? win.loadURL(workspace) : showPrompt();
}

const showPrompt = () => win.loadFile('prompt.html', { query: { prompt: '1' } });

ipcMain.handle('set-workspace', (_e, raw) => {
  let u;
  try {
    u = new URL(/^https?:\/\//.test(raw) ? raw : 'https://' + raw);
  } catch {
    return 'That is not a valid address.';
  }
  if (!u.hostname.includes('.') && u.hostname !== 'localhost') return 'Enter your workspace address, e.g. acme.example.com';
  workspace = u.origin;
  writeConfig({ ...readConfig(), workspace });
  win.loadURL(workspace);
});

Menu.setApplicationMenu(
  Menu.buildFromTemplate([
    ...(process.platform === 'darwin' ? [{ role: 'appMenu' }] : []),
    {
      label: 'File',
      submenu: [
        {
          label: 'Switch Workspace…',
          click: () => {
            workspace = null;
            writeConfig({ ...readConfig(), workspace: null });
            showPrompt();
          },
        },
        { type: 'separator' },
        process.platform === 'darwin' ? { role: 'close' } : { role: 'quit' },
      ],
    },
    { role: 'editMenu' },
    {
      label: 'View',
      submenu: [
        { role: 'reload' },
        { role: 'forceReload' },
        ...(isDev ? [{ role: 'toggleDevTools' }] : []),
        { type: 'separator' },
        { role: 'resetZoom' },
        { role: 'zoomIn' },
        { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
      ],
    },
    { role: 'windowMenu' },
  ])
);

// Unread badge on macOS/Linux docks: poll the same inbox endpoint the web app uses.
const pollInbox = async () => {
  if (!workspace || !win) return;
  try {
    const ses = win.webContents.session;
    const res = await ses.fetch(workspace + '/api/tools/inbox', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{"limit":1}',
    });
    if (res.ok) app.setBadgeCount((await res.json()).unread || 0);
  } catch {
    /* signed out or offline — leave the badge alone */
  }
};

const gotLock = app.requestSingleInstanceLock();
if (!gotLock) app.quit();
app.whenReady().then(() => {
  workspace = readConfig().workspace || null;
  createWindow();
  setInterval(pollInbox, 60_000);
  app.on('second-instance', () => win && (win.isMinimized() && win.restore(), win.focus()));
  app.on('activate', () => BrowserWindow.getAllWindows().length === 0 && createWindow());
});
app.on('window-all-closed', () => process.platform !== 'darwin' && app.quit());
