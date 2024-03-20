import { AnimationEvent } from '@angular/animations';
import { CdkDrag } from '@angular/cdk/drag-drop';
import { CdkPortalOutlet, ComponentPortal, Portal, TemplatePortal } from '@angular/cdk/portal';
import { ChangeDetectorRef, Component, ComponentRef, EmbeddedViewRef, HostBinding, HostListener, InjectionToken, Input, ViewChild } from '@angular/core';
import { Subject } from 'rxjs';
import { dialogAnimation } from './dialog.animations';

export const SFNG_DIALOG_PORTAL = new InjectionToken<Portal<any>>('SfngDialogPortal');

export type SfngDialogState = 'opening' | 'open' | 'closing' | 'closed';

@Component({
  selector: 'sfng-dialog-container',
  template: `
  <div class="container" cdkDrag cdkDragRootElement=".cdk-overlay-pane" [cdkDragDisabled]="!dragable">
    <div *ngIf="dragable" cdkDragHandle id="drag-handle"></div>
    <ng-container cdkPortalOutlet></ng-container>
  </div>
  `,
  animations: [dialogAnimation]
})
export class SfngDialogContainerComponent<T> {
  onStateChange = new Subject<SfngDialogState>();

  ref: ComponentRef<T> | EmbeddedViewRef<T> | null = null;

  constructor(
    private cdr: ChangeDetectorRef,
  ) { }

  @HostBinding('@dialogContainer')
  state = 'enter';

  @ViewChild(CdkPortalOutlet, { static: true })
  _portalOutlet: CdkPortalOutlet | null = null;

  @ViewChild(CdkDrag, { static: true })
  drag!: CdkDrag;

  attachComponentPortal(portal: ComponentPortal<T>): ComponentRef<T> {
    this.ref = this._portalOutlet!.attachComponentPortal(portal)
    return this.ref;
  }

  attachTemplatePortal(portal: TemplatePortal<T>): EmbeddedViewRef<T> {
    this.ref = this._portalOutlet!.attachTemplatePortal(portal);
    return this.ref;
  }

  @Input()
  dragable: boolean = false;

  @HostListener('@dialogContainer.start', ['$event'])
  onAnimationStart({ toState }: AnimationEvent) {
    if (toState === 'enter') {
      this.onStateChange.next('opening');
    } else if (toState === 'exit') {
      this.onStateChange.next('closing');
    }
  }

  @HostListener('@dialogContainer.done', ['$event'])
  onAnimationEnd({ toState }: AnimationEvent) {
    if (toState === 'enter') {
      this.onStateChange.next('open');
    } else if (toState === 'exit') {
      this.onStateChange.next('closed');
    }
  }

  /** Starts the exit animation */
  _startExit() {
    this.state = 'exit';
    this.cdr.markForCheck();
  }
}
