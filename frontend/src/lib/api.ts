declare global { interface Window { go?: any; runtime?: any } }
const backend = () => {
  const app = window.go?.main?.App
  if (!app) throw new Error('The Wails backend is not ready yet')
  return app
}
export async function call<T = any>(name: string, ...args: any[]): Promise<T> {
  return backend()[name](...args)
}
export function on(name: string, fn: (...args: any[]) => void): () => void {
  if (!window.runtime?.EventsOn) return () => {}
  return window.runtime.EventsOn(name, fn)
}
