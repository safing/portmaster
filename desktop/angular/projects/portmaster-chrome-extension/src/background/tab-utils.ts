
/** Queries and returns the currently active tab */
export function getCurrentTab(): Promise<chrome.tabs.Tab> {
  return new Promise((resolve) => {
    chrome.tabs.query({ active: true, lastFocusedWindow: true }, ([tab]) => {
      resolve(tab);
    })
  })
}
