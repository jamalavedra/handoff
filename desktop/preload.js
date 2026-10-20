// Only the built-in workspace prompt gets an API; the remote site sees nothing.
const { contextBridge, ipcRenderer } = require('electron');

if (location.protocol === 'file:' && new URLSearchParams(location.search).has('prompt')) {
  contextBridge.exposeInMainWorld('handoff', {
    setWorkspace: (url) => ipcRenderer.invoke('set-workspace', url),
  });
}
