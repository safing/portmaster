import { Overlay, OverlayConfig, OverlayRef } from '@angular/cdk/overlay';
import { ComponentPortal } from '@angular/cdk/portal';
import { HttpErrorResponse, HttpResponse } from '@angular/common/http';
import { Injectable, InjectionToken, Injector, isDevMode } from '@angular/core';
import { interval, PartialObserver, Subject } from 'rxjs';
import { take, takeUntil } from 'rxjs/operators';
import { IndicatorComponent } from './indicator';

export interface ActionIndicator {
  title: string;
  message?: string;
  status: 'info' | 'success' | 'error';
  timeout?: number;
}

export const ACTION_REF = new InjectionToken<ActionIndicatorRef>('ActionIndicatorRef')
export class ActionIndicatorRef implements ActionIndicator {
  title: string;
  message?: string;
  status: 'info' | 'success' | 'error';
  timeout?: number;

  onClose = new Subject<void>();
  onCloseReplace = new Subject<void>();

  constructor(opts: ActionIndicator, private _overlayRef: OverlayRef) {
    this.title = opts.title;
    this.message = opts.message;
    this.status = opts.status;
    this.timeout = opts.timeout;
  }

  close() {
    this._overlayRef.detach();
    this.onClose.next();
    this.onClose.complete();
  }
}

@Injectable({ providedIn: 'root' })
export class ActionIndicatorService {
  private _activeIndicatorRef: ActionIndicatorRef | null = null;

  constructor(
    private _injector: Injector,
    private overlay: Overlay,
  ) { }

  /**
   * Returns an observer that parses the HTTP API response
   * and shows a success/error action indicator.
   */
  httpObserver(successTitle?: string, errorTitle?: string): PartialObserver<HttpResponse<ArrayBuffer | string>> {
    return {
      next: resp => {
        let msg = this.getErrorMessgae(resp)
        if (!successTitle) {
          successTitle = msg;
          msg = '';
        }
        this.success(successTitle || '', msg)
      },
      error: err => {
        let msg = this.getErrorMessgae(err);
        if (!errorTitle) {
          errorTitle = msg;
          msg = '';
        }
        this.error(errorTitle || '', msg);
      }
    }
  }

  info(title: string, message?: string, timeout?: number) {
    this.create({
      title,
      message: this.ensureMessage(message),
      timeout,
      status: 'info'
    })
  }

  error(title: string, message?: string | any, timeout?: number) {
    this.create({
      title,
      message: this.ensureMessage(message),
      timeout,
      status: 'error'
    })
  }

  success(title: string, message?: string, timeout?: number) {
    this.create({
      title,
      message: this.ensureMessage(message),
      timeout,
      status: 'success'
    })
  }

  /**
   * Creates a new user action indicator.
   *
   * @param msg The action indicator message to show
   */
  async create(msg: ActionIndicator) {
    if (!!this._activeIndicatorRef) {
      this._activeIndicatorRef.onCloseReplace.next();
      await this._activeIndicatorRef.onClose.toPromise();
    }

    const cfg = new OverlayConfig({
      scrollStrategy: this.overlay
        .scrollStrategies.noop(),
      positionStrategy: this.overlay
        .position()
        .global()
        .bottom('2rem')
        .left('5rem'),
    });
    const overlayRef = this.overlay.create(cfg);

    const ref = new ActionIndicatorRef(msg, overlayRef);
    ref.onClose.pipe(take(1)).subscribe(() => {
      if (ref === this._activeIndicatorRef) {
        this._activeIndicatorRef = null;
      }
    })

    // close after the specified time our (or 5000 seconds).
    const timeout = msg.timeout || 5000;
    interval(timeout).pipe(
      takeUntil(ref.onClose),
      take(1),
    ).subscribe(() => {
      ref.close();
    })

    const injector = this.createInjector(ref);
    const portal = new ComponentPortal(
      IndicatorComponent,
      undefined,
      injector
    );
    this._activeIndicatorRef = ref;
    overlayRef.attach(portal);
  }

  /**
   * Creates a new dependency injector that provides msg as
   * ACTION_MESSAGE.
   */
  private createInjector(ref: ActionIndicatorRef): Injector {
    return Injector.create({
      providers: [
        {
          provide: ACTION_REF,
          useValue: ref,
        }
      ],
      parent: this._injector,
    })
  }

  /**
   * Tries to extract a meaningful error message from msg.
   */
  private ensureMessage(msg: string | any): string | undefined {
    if (msg === undefined || msg === null) {
      return undefined;
    }

    if (msg instanceof HttpErrorResponse) {
      return msg.message;
    }

    if (typeof msg === 'string') {
      return msg;
    }

    if (typeof msg === 'object') {
      if ('message' in msg) {
        return msg.message;
      }
      if ('error' in msg) {
        return this.ensureMessage(msg.error);
      }
      if ('toString' in msg) {
        return msg.toString();
      }
    }

    return JSON.stringify(msg);
  }

  /**
   * Coverts an untyped body received by the HTTP API to a string.
   */
  private stringifyBody(body: any): string {
    if (typeof body === 'string') {
      return body;
    }

    if (body instanceof ArrayBuffer) {
      return new TextDecoder('utf-8').decode(body);
    }

    if (typeof body === 'object') {
      return this.ensureMessage(body) || '';
    }
    console.error('unsupported body', body);

    return '';
  }

  /**
   *  @deprecated use the version without a typo ...
   */
  getErrorMessgae(resp: HttpResponse<ArrayBuffer | string> | HttpErrorResponse | Error): string {
    return this.getErrorMessage(resp)
  }

  /**
   * Parses a HTTP or HTTP Error response and returns a
   * message that can be displayed to the user.
   */
  getErrorMessage(resp: HttpResponse<ArrayBuffer | string> | HttpErrorResponse | Error): string {
    try {
      let body: string | null = null;

      if (typeof resp === 'string') {
        return resp
      }

      if (resp instanceof Error) {
        return resp.message;
      }

      if (resp instanceof HttpErrorResponse) {
        // A client-side or network error occured.
        if (resp.error instanceof Error) {
          body = resp.error.message;
        } else {
          body = this.stringifyBody(resp.error);
        }

        if (!!body) {
          body = body[0].toLocaleUpperCase() + body.slice(1)
          return body
        }
      }


      if (resp instanceof HttpResponse) {
        let msg = '';
        const ct = resp.headers.get('content-type') || '';

        body = this.stringifyBody(resp.body);

        if (/application\/json/.test(ct)) {
          if (!!body) {
            msg = body;
          }
        } else if (/text\/plain/.test(ct)) {
          msg = body;
        }

        // Make the first letter uppercase
        if (!!msg) {
          msg = msg[0].toLocaleUpperCase() + msg.slice(1)
          return msg;
        }
      }

      console.error(`Unexpected error type`, resp)

      return `Unknown error: ${resp}`

    } catch (err: any) {
      console.error(err)
      return `Unknown error: ${resp}`
    }
  }
}
