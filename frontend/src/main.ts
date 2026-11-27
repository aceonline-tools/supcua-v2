import { mount, tick } from 'svelte';
import './app.css';
import App from './App.svelte';
import { createAppClient } from './api/bootstrap';
import { AppStore } from './lib/store.svelte';
import { reportMounted, reportRendered } from './lib/diag';
import { createLoader, delayCalls, failCall, trackFirstCalls } from './lib/loader';

const loader = createLoader();
loader.stage('interface');
let raw = await createAppClient();
if (import.meta.env.DEV) {
  const q = new URLSearchParams(location.search);
  raw = failCall(delayCalls(raw, Number(q.get('loaderDelay'))), q.get('loaderFail'));
}
const { client, loads } = trackFirstCalls(raw, ['app.getBootState', 'state.get', 'scripts.list', 'settings.get', 'app.ready']);
const store = new AppStore(client);
const initial = store.init();
mount(App, { target: document.getElementById('app')!, props: { store, copy: async (text: string) => { await client.call('clipboard.write', { text }); } } });
reportMounted(client);
reportRendered(client);
loader.stage('scripts');
void initial.then(() => loads.settled()).then(async (error) => {
  await tick();
  if (error) loader.fail(`Couldn't load your scripts: ${error}`);
  else loader.done();
});
