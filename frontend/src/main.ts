import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';
import { createDefaultClient } from './api/client';
import { AppStore } from './lib/store.svelte';

const client = createDefaultClient();
const store = new AppStore(client);
void store.init();
mount(App, { target: document.getElementById('app')!, props: { store, copy: async (text: string) => { await client.call('clipboard.write', { text }); } } });
