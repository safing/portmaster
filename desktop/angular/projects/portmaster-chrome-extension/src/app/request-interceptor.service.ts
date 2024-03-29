import { Injectable } from "@angular/core";
import { Subject } from "rxjs";



@Injectable({
  providedIn: 'root'
})
export class RequestInterceptorService {
  /** Used to emit when a new URL was requested */
  private onUrlRequested$ = new Subject<chrome.webRequest.WebRequestBodyDetails>();

  /** Used to emit when a URL has likely been blocked by the portmaster */
  private onUrlBlocked$ = new Subject<chrome.webRequest.WebResponseErrorDetails>();

  /** Emits when a new URL was requested */
  get onUrlRequested() {
    return this.onUrlRequested$.asObservable();
  }

  /** Emits when a new URL was likely blocked by the portmaster */
  get onUrlBlocked() {
    return this.onUrlBlocked$.asObservable();
  }

  constructor() {
    this.registerCallbacks()
  }

  private registerCallbacks() {
    const filter = {
      urls: [
        "http://*/*",
        "https://*/*",
      ]
    };

    chrome.webRequest.onBeforeRequest.addListener(details => this.onUrlRequested$.next(details), filter)
    chrome.webRequest.onErrorOccurred.addListener(details => {
      if (details.error !== "net::ERR_ADDRESS_UNREACHABLE") {
        // we don't care about errors other than UNREACHABLE because that's error caused
        // by the portmaster.
        return;
      }

      this.onUrlBlocked$.next(details);
    }, filter)
  }
}
