/* eslint-disable @angular-eslint/no-input-rename */
import { coerceBooleanProperty, coerceNumberProperty } from '@angular/cdk/coercion';
import { ConnectedPosition } from '@angular/cdk/overlay';
import { _getShadowRoot } from '@angular/cdk/platform';
import { DOCUMENT } from '@angular/common';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Directive, ElementRef, HostBinding, HostListener, Inject, Injectable, Injector, Input, NgZone, OnDestroy, Optional, Renderer2, RendererFactory2 } from '@angular/core';
import { Observable, of, Subject } from 'rxjs';
import { debounce, debounceTime, filter, map, skip, take, timeout } from 'rxjs/operators';
import { SfngDialogRef, SfngDialogService } from '../dialog';
import { SfngTipUpAnchorDirective } from './anchor';
import { deepCloneNode, extendStyles, matchElementSize, removeNode } from './clone-node';
import { getCssSelector, synchronizeCssStyles } from './css-utils';
import { SfngTipUpComponent } from './tipup-component';
import { Button, HelpTexts, SFNG_TIP_UP_CONTENTS, TipUp } from './translations';
import { SfngTipUpPlacement, TIPUP_TOKEN } from './utils';

@Directive({
  selector: '[sfngTipUpTrigger]',
})
export class SfngsfngTipUpTriggerDirective implements OnDestroy {
  constructor(
    public readonly elementRef: ElementRef,
    public dialog: SfngDialogService,
    @Optional() @Inject(SfngTipUpAnchorDirective) public anchor: SfngTipUpAnchorDirective | ElementRef<any> | HTMLElement,
    @Inject(SFNG_TIP_UP_CONTENTS) private tipUpContents: HelpTexts<any>,
    private tipupService: SfngTipUpService,
    private cdr: ChangeDetectorRef,
  ) { }

  private dialogRef: SfngDialogRef<SfngTipUpComponent> | null = null;

  /**
   * The helptext token used to search for the tip up defintion.
   */
  @Input('sfngTipUpTrigger')
  set textKey(s: string) {
    if (!!this._textKey) {
      this.tipupService.deregister(this._textKey, this);
    }
    this._textKey = s;
    this.tipupService.register(this._textKey, this);
  }
  get textKey() { return this._textKey; }
  private _textKey: string = '';

  /**
   * The text to display inside the tip up. If unset, the tipup definition
   * will be loaded form helptexts.yaml.
   * This input property is mainly designed for programatic/dynamic tip-up generation
   */
  @Input('sfngTipUpText')
  text: string | undefined;

  @Input('sfngTipUpTitle')
  title: string | undefined;

  @Input('sfngTipUpButtons')
  buttons: Button<any>[] | undefined;

  /**
   * asTipUp returns a tip-up definition built from the input
   * properties sfngTipUpText and sfngTipUpTitle. If none are set
   * then null is returned.
   */
  asTipUp(): TipUp<any> | null {
    // TODO(ppacher): we could also merge the defintions from MyYamlFile
    // and the properties set on this directive....
    if (!this.text) {
      return this.tipUpContents[this.textKey];
    }
    return {
      title: this.title || '',
      content: this.text,
      buttons: this.buttons,
    }
  }

  /**
   * The default anchor for the tipup if non is provided via Dependency-Injection
   * or using sfngTipUpAnchorRef
   */
  @Input('sfngTipUpDefaultAnchor')
  defaultAnchor: ElementRef<any> | HTMLElement | null = null;

  /** Optionally overwrite the anchor element received via Dependency Injection */
  @Input('sfngTipUpAnchorRef')
  set anchorRef(ref: ElementRef<any> | HTMLElement | null) {
    this.anchor = ref ?? this.anchor;
  }

  /** Used to ensure all tip-up triggers have a pointer cursor */
  @HostBinding('style.cursor')
  cursor = 'pointer';

  /** De-register ourself upon destroy */
  ngOnDestroy() {
    this.tipupService.deregister(this.textKey, this);
  }

  /** Whether or not we're passive-only and thus do not handle click-events form the user */
  @Input('sfngTipUpPassive')
  set passive(v: any) {
    this._passive = coerceBooleanProperty(v ?? true);
  }
  get passive() { return this._passive; }
  private _passive = false;

  @Input('sfngTipUpOffset')
  set offset(v: any) {
    this._defaultOffset = coerceNumberProperty(v)
  }
  get offset() { return this._defaultOffset }
  private _defaultOffset = 20;

  @Input('sfngTipUpPlacement')
  placement: SfngTipUpPlacement | null = null;

  @HostListener('click', ['$event'])
  onClick(event?: MouseEvent): Promise<any> {
    if (!!event) {
      // if there's a click event the user actually clicked the element.
      // we only handle this if we're not marked as passive.
      if (this._passive) {
        return Promise.resolve();
      }

      event.preventDefault();
      event.stopPropagation();
    }

    if (!!this.dialogRef) {
      this.dialogRef.close();
      return Promise.resolve();
    }

    let anchorElement: ElementRef<any> | HTMLElement | null = this.defaultAnchor || this.elementRef;
    let placement: SfngTipUpPlacement | null = this.placement;

    if (!!this.anchor) {
      if (this.anchor instanceof SfngTipUpAnchorDirective) {
        anchorElement = this.anchor.elementRef;
        placement = this.anchor;
      } else {
        anchorElement = this.anchor;
      }
    }

    this.dialogRef = this.tipupService.createTipup(
      anchorElement,
      this.textKey,
      this,
      placement,
    )

    this.dialogRef.onClose
      .pipe(take(1))
      .subscribe(() => {
        this.dialogRef = null;
        this.cdr.markForCheck();
      });

    this.cdr.detectChanges();

    return this.dialogRef.onStateChange
      .pipe(
        filter(state => state === 'opening'),
        take(1),
      )
      .toPromise()
  }
}

@Component({
  selector: 'sfng-tipup',
  template:
    `<svg viewBox="0 0 24 24"
    class="tipup"
    [sfngTipUpTrigger]="key"
    [sfngTipUpDefaultAnchor]="parent"
    [sfngTipUpPlacement]="placement"
    [sfngTipUpText]="text"
    [sfngTipUpTitle]="title"
    [sfngTipUpButtons]="buttons"
    [sfngTipUpAnchorRef]="anchor">
    <g fill="none" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" >
      <path stroke="#ffff" shape-rendering="geometricPrecision" d="M12 21v0c-4.971 0-9-4.029-9-9v0c0-4.971 4.029-9 9-9v0c4.971 0 9 4.029 9 9v0c0 4.971-4.029 9-9 9z"/>
      <path stroke="#ffff" shape-rendering="geometricPrecision" d="M12 17v-5h-1M11.749 8c-.138 0-.25.112-.249.25 0 .138.112.25.25.25s.25-.112.25-.25-.112-.25-.251-.25"/>
    </g>
  </svg>`,
  styles: [
    `
      :host {
        display: inline-block;
        width   : 1rem;
        position: relative;
        opacity: 0.55;
        cursor  : pointer;
        align-self: center;
      }

      :host:hover {
        opacity: 1;
      }
      `
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngTipUpIconComponent implements SfngTipUpPlacement {
  @Input()
  key: string = '';

  // see sfngTipUpTrigger sfngTipUpText and sfngTipUpTitle
  @Input() text: string | undefined = undefined;
  @Input() title: string | undefined = undefined;
  @Input() buttons: Button<any>[] | undefined = undefined;

  @Input()
  anchor: ElementRef<any> | HTMLElement | null = null;

  @Input('placement')
  origin: 'left' | 'right' = 'right';

  @Input()
  set offset(v: any) {
    this._offset = coerceNumberProperty(v);
  }
  get offset() { return this._offset; }
  private _offset: number = 10;

  constructor(private elementRef: ElementRef<any>) { }

  get placement(): SfngTipUpPlacement {
    return this
  }

  get parent(): HTMLElement | null {
    return (this.elementRef?.nativeElement as HTMLElement)?.parentElement;
  }
}


@Injectable({
  providedIn: 'root'
})
export class SfngTipUpService {
  tipups = new Map<string, SfngsfngTipUpTriggerDirective>();

  private _onRegister = new Subject<string>();
  private _onUnregister = new Subject<string>();

  get onRegister(): Observable<string> {
    return this._onRegister.asObservable();
  }

  get onUnregister(): Observable<string> {
    return this._onUnregister.asObservable();
  }

  waitFor(key: string): Observable<void> {
    if (this.tipups.has(key)) {
      return of(undefined);
    }

    return this.onRegister
      .pipe(
        filter(val => val === key),
        debounce(() => this.ngZone.onStable.pipe(skip(2))),
        debounceTime(1000),
        take(1),
        map(() => { }),
        timeout(5000),
      );
  }

  private renderer: Renderer2;

  constructor(
    @Inject(DOCUMENT) private _document: Document,
    private dialog: SfngDialogService,
    private ngZone: NgZone,
    private injector: Injector,
    rendererFactory: RendererFactory2
  ) {
    this.renderer = rendererFactory.createRenderer(null, null)
  }

  register(key: string, trigger: SfngsfngTipUpTriggerDirective) {
    if (this.tipups.has(key)) {
      return;
    }

    this.tipups.set(key, trigger);
    this._onRegister.next(key);
  }

  deregister(key: string, trigger: SfngsfngTipUpTriggerDirective) {
    if (this.tipups.get(key) === trigger) {
      this.tipups.delete(key);
      this._onUnregister.next(key);
    }
  }

  getTipUp(key: string): TipUp<any> | null {
    return this.tipups.get(key)?.asTipUp() || null;
  }

  private _latestTipUp: SfngDialogRef<SfngTipUpComponent> | null = null;

  createTipup(
    anchor: HTMLElement | ElementRef<any>,
    key: string,
    origin?: SfngsfngTipUpTriggerDirective,
    opts: SfngTipUpPlacement | null = {},
    injector?: Injector): SfngDialogRef<SfngTipUpComponent> {

    const lastTipUp = this._latestTipUp
    let closePrevious = () => {
      if (!!lastTipUp) {
        lastTipUp.close();
      }
    }

    // make sure we have an ElementRef to work with
    if (!(anchor instanceof ElementRef)) {
      anchor = new ElementRef(anchor)
    }

    // the the origin placement of the tipup
    const positions: ConnectedPosition[] = [];
    if (opts?.origin === 'left') {
      positions.push({
        originX: 'start',
        originY: 'center',
        overlayX: 'end',
        overlayY: 'center',
      })
    } else {
      positions.push({
        originX: 'end',
        originY: 'center',
        overlayX: 'start',
        overlayY: 'center',
      })
    }

    // determine the offset to the tipup origin
    let offset = opts?.offset ?? 10;
    if (opts?.origin === 'left') {
      offset *= -1;
    }

    let postitionStrategy = this.dialog.position()
      .flexibleConnectedTo(anchor)
      .withPositions(positions)
      .withDefaultOffsetX(offset);

    const inj = Injector.create({
      providers: [
        {
          useValue: key,
          provide: TIPUP_TOKEN,
        }
      ],
      parent: injector || this.injector,
    });


    const newTipUp = this.dialog.create(SfngTipUpComponent, {
      dragable: false,
      autoclose: true,
      backdrop: 'light',
      injector: inj,
      positionStrategy: postitionStrategy
    });
    this._latestTipUp = newTipUp;

    const _preview = this._createPreview(anchor.nativeElement, _getShadowRoot(anchor.nativeElement));

    // construct a CSS selector that targets the clicked origin (sfngTipUpTriggerDirective) from within
    // the anchor. We use that path to highlight the copy of the trigger-directive in the preview.
    if (!!origin) {
      const originSelector = getCssSelector(origin.elementRef.nativeElement, anchor.nativeElement);
      let target: HTMLElement | null = null;
      if (!!originSelector) {
        target = _preview.querySelector(originSelector);
      } else {
        target = _preview;
      }

      this.renderer.addClass(target, 'active-tipup-trigger')
    }

    newTipUp.onStateChange
      .pipe(
        filter(state => state === 'open'),
        take(1)
      )
      .subscribe(() => {
        closePrevious();
        _preview.attach()
      })

    newTipUp.onStateChange
      .pipe(
        filter(state => state === 'closing'),
        take(1)
      )
      .subscribe(() => {
        if (this._latestTipUp === newTipUp) {
          this._latestTipUp = null;
        }
        _preview.classList.remove('visible');
        setTimeout(() => {
          removeNode(_preview);
        }, 300)
      });

    return newTipUp;
  }

  private _createPreview(element: HTMLElement, shadowRoot: ShadowRoot | null): HTMLElement & { attach: () => void } {
    const preview = deepCloneNode(element);
    // clone all CSS styles by applying them directly to the copied
    // nodes. Though, we skip the opacity property because we use that
    // a lot and it makes the preview strange ....
    synchronizeCssStyles(element, preview, new Set([
      'opacity'
    ]));

    // make sure the preview element is at the exact same position
    // as the original one.
    matchElementSize(preview, element.getBoundingClientRect());

    extendStyles(preview.style, {
      // We have to reset the margin, because it can throw off positioning relative to the viewport.
      'margin': '0',
      'position': 'fixed',
      'top': '0',
      'left': '0',
      'z-index': '1000',
      'opacity': 'unset',
    }, new Set(['position']));

    // We add a dedicated class to the preview element so
    // it can handle special higlighting itself.
    preview.classList.add('tipup-preview')

    // since the user might want to click on the preview element we must
    // intercept the click-event, determine the path to the target element inside
    // the preview and eventually dispatch a click-event on the actual
    // - real - target inside the cloned element.
    preview.onclick = function (event: MouseEvent) {
      let path = getCssSelector(event.target as HTMLElement, preview);
      if (!!path) {
        // find the target by it's CSS path
        let actualTarget: HTMLElement | null = element.querySelector<HTMLElement>(path);

        // some (SVG) elements don't have a direct click() listener so we need to search
        // the parents upwards to find one that implements click().
        // we're basically searching up until we reach the <html> tag.
        //
        // TODO(ppacher): stop searching at the respective root node.
        if (!!actualTarget) {
          let iter: HTMLElement = actualTarget;
          while (iter != null) {
            if ('click' in iter && typeof iter['click'] === 'function') {
              iter.click();
              break;
            }
            iter = iter.parentNode as HTMLElement;
          }
        }
      } else {
        // the user clicked the preview element directly
        try {
          element.click()
        } catch (e) {
          console.error(e);
        }
      }
    }

    let attach = () => {
      const parent = this._getPreviewInserationPoint(shadowRoot)
      const cdkOverlayContainer = parent.getElementsByClassName('cdk-overlay-container')[0]
      // if we find a cdkOverlayContainer in our inseration point (which we expect to be there)
      // we insert the preview element right after the overlay-backdrop. This way the tip-up
      // dialog will still be on top of the preview.
      if (!!cdkOverlayContainer) {
        const reference = cdkOverlayContainer.getElementsByClassName("cdk-overlay-backdrop")[0].nextSibling;
        cdkOverlayContainer.insertBefore(preview, reference)
      } else {
        parent.appendChild(preview);
      }

      setTimeout(() => {
        preview.classList.add('visible');
      })
    }

    Object.defineProperty(preview, 'attach', {
      value: attach,
    })

    return preview as any;
  }

  private _getPreviewInserationPoint(shadowRoot: ShadowRoot | null): HTMLElement {
    const documentRef = this._document;
    return shadowRoot ||
      documentRef.fullscreenElement ||
      (documentRef as any).webkitFullscreenElement ||
      (documentRef as any).mozFullScreenElement ||
      (documentRef as any).msFullscreenElement ||
      documentRef.body;
  }

  async open(key: string) {
    const comp = this.tipups.get(key);
    if (!comp) {
      console.error('Tried to open unknown tip-up with key ' + key);
      return;
    }
    comp.onClick()
  }
}
