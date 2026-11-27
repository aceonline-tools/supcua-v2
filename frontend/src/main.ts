import { mount, tick } from 'svelte';
import './app.css';
import App from './App.svelte';
import { createAppClient } from './api/bootstrap';
import { AppStore } from './lib/store.svelte';
import { reportMounted, reportRendered } from './lib/diag';
import { createLoader, delayCalls } from './lib/loader';

const loader = createLoader();
loader.stage('interface');
let client = await createAppClient();
if (import.meta.env.DEV) client = delayCalls(client, Number(new URLSearchParams(location.search).get('loaderDelay')));
const store = new AppStore(client);
const initial = store.init();
mount(App, { target: document.getElementById('app')!, props: { store, copy: async (text: string) => { await client.call('clipboard.write', { text }); } } });
reportMounted(client);
reportRendered(client);
loader.stage('scripts');
void initial.finally(async () => { await tick(); loader.done(); });
