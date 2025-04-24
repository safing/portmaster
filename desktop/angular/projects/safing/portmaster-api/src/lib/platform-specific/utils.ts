// Simple function to detect if the app is running in a Tauri environment
export function IsTauriEnvironment(): boolean {
  return '__TAURI__' in window;
}
