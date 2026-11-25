import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';
import { createAppClient } from './api/bootstrap';
import { AppStore } from './lib/store.svelte';

const client = await createAppClient();
const store = new AppStore(client);
void store.init();
mount(App, { target: document.getElementById('app')!, props: { store, copy: async (text: string) => { await client.call('clipboard.write', { text }); } } });
