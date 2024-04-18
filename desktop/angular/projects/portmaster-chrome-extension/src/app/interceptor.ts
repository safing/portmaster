import { HttpEvent, HttpHandler, HttpInterceptor, HttpRequest } from "@angular/common/http";
import { Injectable } from "@angular/core";
import { BehaviorSubject, filter, Observable, switchMap } from "rxjs";


@Injectable()
export class AuthIntercepter implements HttpInterceptor {
  /** Used to delay requests until we loaded the access token from the extension storage. */
  private loaded$ = new BehaviorSubject<boolean>(false);

  /** Holds the access token required to talk to the Portmaster API. */
  private token: string | null = null;

  constructor() {
    // make sure we use the new access token once we get one.
    chrome.storage.onChanged.addListener(changes => {
      this.token = changes['key'].newValue || null;
    })

    // try to read the current access token from the extension storage.
    chrome.storage.local.get('key', obj => {
      this.token = obj.key || null;
      console.log("got token", this.token)
      this.loaded$.next(true);
    })

    chrome.runtime.sendMessage({ type: 'listRequests', tabId: 'current' }, (response: any) => {
      console.log(response);
    })
  }

  intercept(req: HttpRequest<any>, next: HttpHandler): Observable<HttpEvent<any>> {
    return this.loaded$.pipe(
      filter(loaded => loaded),
      switchMap(() => {
        if (!!this.token) {
          req = req.clone({
            headers: req.headers.set("Authorization", "Bearer " + this.token)
          })
        }
        return next.handle(req)
      })
    )
  }
}
