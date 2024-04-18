import { Overlay, OverlayConfig, OverlayPositionBuilder, PositionStrategy } from '@angular/cdk/overlay';
import { ComponentPortal, ComponentType, TemplatePortal } from '@angular/cdk/portal';
import { EmbeddedViewRef, Injectable, Injector } from '@angular/core';
import { filter, take, takeUntil } from 'rxjs/operators';
import { ConfirmDialogConfig, CONFIRM_DIALOG_CONFIG, SfngConfirmDialogComponent } from './confirm.dialog';
import { SfngDialogContainerComponent } from './dialog.container';
import { SfngDialogModule } from './dialog.module';
import { SfngDialogRef, SFNG_DIALOG_REF } from './dialog.ref';

export interface BaseDialogConfig {
  /** whether or not the dialog should close on outside-clicks and ESC */
  autoclose?: boolean;

  /** whether or not a backdrop should be visible */
  backdrop?: boolean | 'light';

  /** whether or not the dialog should be dragable */
  dragable?: boolean;

  /**
   * optional position strategy for the overlay. if omitted, the
   * overlay will be centered on the screen
   */
  positionStrategy?: PositionStrategy;

  /**
   * Optional data for the dialog that is available either via the
   * SfngDialogRef for ComponentPortals as an $implicit context value
   * for TemplatePortals.
   *
   * Note, for template portals, data is only set as an $implicit context
   * value if it is not yet set in the portal!
   */
  data?: any;
}

export interface ComponentPortalConfig {
  injector?: Injector;
}

@Injectable({ providedIn: SfngDialogModule })
export class SfngDialogService {

  constructor(
    private injector: Injector,
    private overlay: Overlay,
  ) { }

  position(): OverlayPositionBuilder {
    return this.overlay.position();
  }

  create<T>(template: TemplatePortal<T>, opts?: BaseDialogConfig): SfngDialogRef<EmbeddedViewRef<T>>;
  create<T>(target: ComponentType<T>, opts?: BaseDialogConfig & ComponentPortalConfig): SfngDialogRef<T>;
  create<T>(target: ComponentType<T> | TemplatePortal<T>, opts: BaseDialogConfig & ComponentPortalConfig = {}): SfngDialogRef<any> {
    let position: PositionStrategy = opts?.positionStrategy || this.overlay
      .position()
      .global()
      .centerVertically()
      .centerHorizontally();

    let hasBackdrop = true;
    let backdropClass = 'dialog-screen-backdrop';
    if (opts.backdrop !== undefined) {
      if (opts.backdrop === false) {
        hasBackdrop = false;
      } else if (opts.backdrop === 'light') {
        backdropClass = 'dialog-screen-backdrop-light';
      }
    }

    const cfg = new OverlayConfig({
      scrollStrategy: this.overlay.scrollStrategies.noop(),
      positionStrategy: position,
      hasBackdrop: hasBackdrop,
      backdropClass: backdropClass,
    });
    const overlayref = this.overlay.create(cfg);

    // create our dialog container and attach it to the
    // overlay.
    const containerPortal = new ComponentPortal<SfngDialogContainerComponent<T>>(
      SfngDialogContainerComponent,
      undefined,
      this.injector,
    )
    const containerRef = containerPortal.attach(overlayref);

    if (!!opts.dragable) {
      containerRef.instance.dragable = true;
    }

    // create the dialog ref
    const dialogRef = new SfngDialogRef<T>(overlayref, containerRef.instance, opts.data);

    // prepare the content portal and attach it to the container
    let result: any;
    if (target instanceof TemplatePortal) {
      let r = containerRef.instance.attachTemplatePortal(target)

      if (!!r.context && typeof r.context === 'object' && !('$implicit' in r.context)) {
        r.context = {
          $implicit: opts.data,
          ...r.context,
        }
      }

      result = r
    } else {
      const contentPortal = new ComponentPortal(target, null, Injector.create({
        providers: [
          {
            provide: SFNG_DIALOG_REF,
            useValue: dialogRef,
          }
        ],
        parent: opts?.injector || this.injector,
      }));
      result = containerRef.instance.attachComponentPortal(contentPortal);
    }
    // update the container position now that we have some content.
    overlayref.updatePosition();

    if (!!opts?.autoclose) {
      overlayref.outsidePointerEvents()
        .pipe(take(1))
        .subscribe(() => dialogRef.close());
      overlayref.keydownEvents()
        .pipe(
          takeUntil(overlayref.detachments()),
          filter(event => event.key === 'Escape')
        )
        .subscribe(() => {
          dialogRef.close();
        })
    }
    return dialogRef;
  }

  confirm(opts: ConfirmDialogConfig): SfngDialogRef<SfngConfirmDialogComponent, string> {
    return this.create(SfngConfirmDialogComponent, {
      autoclose: opts.canCancel,
      injector: Injector.create({
        providers: [
          {
            provide: CONFIRM_DIALOG_CONFIG,
            useValue: opts,
          },
        ],
        parent: this.injector,
      })
    })
  }
}
