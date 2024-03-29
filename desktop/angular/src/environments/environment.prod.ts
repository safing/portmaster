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