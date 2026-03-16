/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}

declare module 'cytoscape' {
  const cytoscape: any
  export default cytoscape
}

declare module 'vue-echarts' {
  import { DefineComponent } from 'vue'
  const VChart: DefineComponent<any, any, any>
  export default VChart
}
