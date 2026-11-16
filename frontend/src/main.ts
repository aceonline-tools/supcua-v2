import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';
import { createDefaultClient } from './api/client';
import { AppStore } from './lib/store.svelte';

const store = new AppStore(createDefaultClient());
void store.init();
mount(App, { target: document.getElementById('app')!, props: { store } });
