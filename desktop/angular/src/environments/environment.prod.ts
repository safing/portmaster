/*
export const environment = new class {
  readonly supportHub = "https://support.safing.io"
  readonly production = true;

  get httpAPI() {
    return `http://${window.location.host}/api`
  }

  get portAPI() {
    const result = `ws://${window.location.host}/api/database/v1`;
    return result;
  }
}
*/

export const environment = {
  production: false,
  portAPI: "ws://127.0.0.1:817/api/database/v1",
  httpAPI: "http://127.0.0.1:817/api",
  supportHub: "https://support.safing.io"
};
