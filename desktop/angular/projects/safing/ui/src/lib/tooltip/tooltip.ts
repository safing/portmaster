/* eslint-disable @angular-eslint/no-input-rename */
import { coerceNumberProperty } from "@angular/cdk/coercion";
import { ConnectedPosition, Overlay, OverlayRef, PositionStrategy } from "@angular/cdk/overlay";
import { ComponentPortal } from "@angular/cdk/portal";
import { ComponentRef, Directive, ElementRef, HostListener, Injector, Input, isDevMode, OnChanges, OnDestroy, OnInit, TemplateRef } from "@angular/core";
import { Subject } from "rxjs";
import { SfngTooltipComponent, SFNG_TOOLTIP_CONTENT, SFNG_TOOLTIP_OVERLAY } from "./tooltip-component";

/** The allowed tooltip positions. */
export type SfngTooltipPosition = 'left' | 'right' | 'bottom' | 'top';

@Directive({
  selector: '[sfng-tooltip],[snfgTooltip]',
})
export class SfngTooltipDirective implements OnInit, OnDestroy, OnChanges {
  /** Used to control the visibility of the tooltip */
  private attach$ = new Subject<boolean>();

  /** Holds a reference to the tooltip overlay */
  private tooltipRef: ComponentRef<SfngTooltipComponent> | null = null;

  /**
   * A reference to a timeout created by setTimeout used to debounce
   * displaying the tooltip
   */
  private debouncer: any | null = null;

  constructor(
    private overlay: Overlay,
    private injector: Injector,
    private originRef: ElementRef<any>,
  ) { }

  @HostListener('mouseenter')
  show(delay = this.delay) {
    if (this.debouncer !== null) {
      clearTimeout(this.debouncer);
    }

    this.debouncer = setTimeout(() => {
      this.debouncer = null;
      this.attach$.next(true);
    }, delay);
  }

  @HostListener('mouseleave')
  hide(delay = this.delay / 2) {
    // if we're currently debouncing a "show" than
    // we should clear that out to avoid re-attaching
    // the tooltip right after we disposed it.
    if (this.debouncer !== null) {
      clearTimeout(this.debouncer);
      this.debouncer = null;
    }

    this.debouncer = setTimeout(() => {
      this.attach$.next(false);
      this.debouncer = null;
    }, delay);
  }

  /** Debounce delay before showing the tooltip */
  @Input('sfngTooltipDelay')
  set delay(v: any) {
    this._delay = coerceNumberProperty(v);
  }
  get delay() { return this._delay }
  private _delay = 500;

  /** An additional offset between the tooltip overlay and the origin centers */
  @Input('sfngTooltipOffset')
  set offset(v: any) {
    this._offset = coerceNumberProperty(v);
  }
  private _offset: number | null = 8;

  /** The actual content that should be displayed in the tooltip overlay. */
  @Input('sfngTooltip')
  @Input('sfng-tooltip')
  tooltipContent: string | TemplateRef<any> | null = null;

  @Input('snfgTooltipPosition')
  position: ConnectedPosition | SfngTooltipPosition | (SfngTooltipPosition | ConnectedPosition)[] | 'any' = 'any';

  ngOnInit() {
    this.attach$
      .subscribe(attach => {
        if (attach) {
          this.createTooltip();
          return;
        }
        if (!!this.tooltipRef) {
          this.tooltipRef.instance.dispose();
          this.tooltipRef = null;
        }
      })
  }

  ngOnDestroy(): void {
    this.attach$.next(false);
    this.attach$.complete();
  }

  ngOnChanges(): void {
    // if the tooltip content has be set to null and we're still
    // showing the tooltip we treat that as an attempt to hide.
    if (this.tooltipContent === null && !!this.tooltipRef) {
      this.hide();
    }
  }

  /** Creates the actual tooltip overlay */
  private createTooltip() {
    // there's nothing to do if the tooltip is still active.
    if (!!this.tooltipRef) {
      return;
    }

    // support disabling the tooltip by passing "null" for
    // the content.
    if (this.tooltipContent === null) {
      return;
    }

    const position = this.buildPositionStrategy();

    const overlayRef = this.overlay.create({
      positionStrategy: position,
      scrollStrategy: this.overlay.scrollStrategies.close(),
      disposeOnNavigation: true,
    });

    // make sure we close the tooltip if the user clicks on our
    // originRef.
    overlayRef.outsidePointerEvents()
      .subscribe(() => this.hide());

    overlayRef.attachments()
      .subscribe(() => {
        if (!overlayRef) {
          return
        }
        overlayRef.updateSize({});
        overlayRef.updatePosition();
      })

    // create a component portal for the tooltip component
    // and attach it to our newly created overlay.
    const portal = this.getOverlayPortal(overlayRef);
    this.tooltipRef = overlayRef.attach(portal);
  }

  private getOverlayPortal(ref: OverlayRef): ComponentPortal<SfngTooltipComponent> {
    const inj = Injector.create({
      providers: [
        { provide: SFNG_TOOLTIP_CONTENT, useValue: this.tooltipContent },
        { provide: SFNG_TOOLTIP_OVERLAY, useValue: ref },
      ],
      parent: this.injector,
      name: 'SfngTooltipDirective'
    })

    const portal = new ComponentPortal(
      SfngTooltipComponent,
      undefined,
      inj
    )

    return portal;
  }

  /** Builds a FlexibleConnectedPositionStrategy for the tooltip overlay */
  private buildPositionStrategy(): PositionStrategy {
    let pos = this.position;
    if (pos === 'any') {
      pos = ['top', 'bottom', 'right', 'left']
    } else if (!Array.isArray(pos)) {
      pos = [pos];
    }

    let allowedPositions: ConnectedPosition[] =
      pos.map(p => {
        if (typeof p === 'string') {
          return this.getAllowedConnectedPosition(p);
        }
        // this is already a ConnectedPosition
        return p
      });

    let position = this.overlay.position()
      .flexibleConnectedTo(this.originRef)
      .withFlexibleDimensions(true)
      .withPush(true)
      .withPositions(allowedPositions)
      .withGrowAfterOpen(true)
      .withTransformOriginOn('.sfng-tooltip-instance')

    return position;
  }

  private getAllowedConnectedPosition(type: SfngTooltipPosition): ConnectedPosition {
    switch (type) {
      case 'left':
        return {
          originX: 'start',
          originY: 'center',
          overlayX: 'end',
          overlayY: 'center',
          offsetX: - (this._offset || 0),
        }
      case 'right':
        return {
          originX: 'end',
          originY: 'center',
          overlayX: 'start',
          overlayY: 'center',
          offsetX: (this._offset || 0),
        }
      case 'top':
        return {
          originX: 'center',
          originY: 'top',
          overlayX: 'center',
          overlayY: 'bottom',
          offsetY: - (this._offset || 0),
        }
      case 'bottom':
        return {
          originX: 'center',
          originY: 'bottom',
          overlayX: 'center',
          overlayY: 'top',
          offsetY: (this._offset || 0),
        }
      default:
        if (isDevMode()) {
          throw new Error(`invalid value for SfngTooltipPosition: ${type}`)
        }
        // fallback to "right"
        return this.getAllowedConnectedPosition('right')
    }
  }
}

