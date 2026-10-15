import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import Portal from './Portal.vue'
import './style.css'

createApp(Portal).use(ElementPlus).mount('#portal')
