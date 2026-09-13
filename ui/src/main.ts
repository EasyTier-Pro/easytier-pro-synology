import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { applyStoredTheme } from './theme'
import './styles/main.css'

// The inline script in index.html painted the background from the stored choice;
// this puts the interface's own state in step with it.
applyStoredTheme()

createApp(App).use(router).mount('#app')
