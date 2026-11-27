import { mount, tick } from 'svelte';
import './app.css';
import App from './App.svelte';
import { createAppClient } from './api/bootstrap';
import EditorApp from './components/EditorApp.svelte';
import { AppStore } from './lib/store.svelte';
import { pageRole } from './lib/role';
import { startEditorWindow } from './lib/editorWindow';
import { reportMounted, reportRendered } from './lib/diag';
import { createLoader, delayCalls, failCall, reportLoader, trackFirstCalls } from './lib/loader';

const loader = createLoader();
loader.stage('interface');
let raw = await createAppClient();
if (import.meta.env.DEV) {
  const q = new URLSearchParams(location.search);
  raw = failCall(delayCalls(raw, Number(q.get('loaderDelay'))), q.get('loaderFail'));
}
const { client, loads } = trackFirstCalls(raw, ['app.getBootState', 'state.get', 'scripts.list', 'settings.get', 'app.ready']);
const role = pageRole();
const store = new AppStore(client, role);
const initial = store.init();
let ready: Promise<unknown> = initial;
if (role === 'editor') {
  const win = startEditorWindow(client, store, initial);
  ready = win.opened;
  mount(EditorApp, { target: document.getElementById('app')!, props: { store } });
} else {
  mount(App, { target: document.getElementById('app')!, props: { store, copy: async (text: string) => { await client.call('clipboard.write', { text }); } } });
}
reportMounted(client);
reportRendered(client);
loader.stage('scripts');
void ready.then(() => loads.settled()).then(async (error) => {
  await tick();
  // A not-licensed host only serves app.getBootState/app.ready, so the other initial loads fail by design there.
  const failed = !!error && !loads.notLicensed();
  if (failed) loader.fail(`Couldn't load your scripts: ${error}`);
  else loader.done();
  reportLoader(client, failed ? 'error' : 'done');
});
