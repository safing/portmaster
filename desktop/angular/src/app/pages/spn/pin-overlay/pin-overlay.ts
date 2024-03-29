import { AnimationEvent, animate, keyframes, style, transition, trigger } from '@angular/animations';
import { CdkDrag, CdkDragHandle, CdkDragRelease } from '@angular/cdk/drag-drop';
import { Overlay, OverlayRef, PositionStrategy } from '@angular/cdk/overlay';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, EventEmitter, HostListener, Inject, Input, OnInit, Output, ViewChild, inject } from '@angular/core';
import { Router } from '@angular/router';
import { SfngDialogService } from '@safing/ui';
import { PinDetailsComponent } from '../pin-details';
import { MapOverlay, Path } from '../spn-page';
import { ActionIndicatorService } from './../../../shared/action-indicator/action-indicator.service';
import { MapPin } from './../map.service';
import { OVERLAY_REF } from './../utils';
import { INTEGRATION_SERVICE } from 'src/app/integration';

export interface PinOverlayHoverEvent {
  type: 'enter' | 'leave';
  pinID: string;
}

@Component({
  templateUrl: './pin-overlay.html',
  styleUrls: [
    './pin-overlay.scss'
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    trigger('moveIn', [
      transition(':enter', [
        style({ transform: 'scale(0)', transformOrigin: 'top left' }),
        animate('200ms {{ delay }}ms cubic-bezier(0, 0, 0.2, 1)',
          keyframes([
            style({ transform: 'scaleX(1) scaleY(0.1)', transformOrigin: 'top left', offset: 0.3 }),
            style({ transform: 'scaleX(1) scaleY(1)', transformOrigin: 'top left', offset: 0.8 }),
          ])
        )
      ], { params: { delay: "0" } }),
      transition(':leave', [
        style({ transform: 'scale(1)', opacity: 1, transformOrigin: 'top left' }),
        animate('500ms cubic-bezier(0, 0, 0.2, 1)',
          keyframes([
            style({ transform: 'scaleX(1) scaleY(0.1)', opacity: 0.5, transformOrigin: 'top left', offset: 0.3 }),
            style({ transform: 'scaleX(0) scaleY(0)', opacity: 0, transformOrigin: 'top left', offset: 0.8 }),
          ])
        )
      ])
    ])
  ]
})
export class PinOverlayComponent implements OnInit {
  private readonly integration = inject(INTEGRATION_SERVICE);

  @Input()
  mapPin!: MapPin;

  @Input()
  routeHome?: Path;

  @Input()
  additionalPaths?: Path[] = [];

  @Input()
  delay: number = 0;

  @Output()
  afterDispose = new EventEmitter<string>();

  @Output()
  overlayHover = new EventEmitter<PinOverlayHoverEvent>();

  @ViewChild(CdkDrag)
  dragContainer!: CdkDrag;

  @ViewChild(CdkDragHandle)
  dragHandle!: CdkDragHandle;

  showContent = false;

  /** Indicates whether or not the pin overlay has been moved by the user */
  hasBeenMoved = false;

  private oldPositionStrategy?: PositionStrategy;

  @HostListener('mouseenter')
  onHostElementMouseEnter(event: MouseEvent) {
    this.overlayHover.next({
      type: 'enter',
      pinID: this.mapPin.pin.ID
    })

    this.containerClass = '';
  }

  @HostListener('mouseleave')
  onHostElementMouseLeave(event: MouseEvent) {
    this.overlayHover.next({
      type: 'leave',
      pinID: this.mapPin.pin.ID
    })

    this.containerClass = 'bg-opacity-90'
  }

  /** on double-click, restore the old pin overlay position (before being initialy dragged by the user) */
  onDragDblClick() {
    if (!!this.oldPositionStrategy) {
      this.overlayRef.updatePositionStrategy(this.oldPositionStrategy);
      this.overlayRef.updatePosition();
      this.hasBeenMoved = false;
    }
  }

  onDragStart() {
    this.containerClass = 'outline'
  }

  openPinDetails() {
    this.dialog.create(PinDetailsComponent, {
      data: this.mapPin.pin.ID,
      autoclose: true,
      backdrop: false,
      dragable: true,
    })
  }

  onDragRelease(event: CdkDragRelease) {
    if (!this.dragContainer || !this.overlayRef.hostElement || !this.overlayRef.hostElement.parentElement) {
      return;
    }

    const bbox = this.dragContainer.element.nativeElement.getBoundingClientRect();
    const parent = this.overlayRef.hostElement.parentElement!.getBoundingClientRect();

    if (!this.oldPositionStrategy) {
      this.oldPositionStrategy = this.overlayRef.getConfig().positionStrategy;
    }

    this.containerClass = '';

    this.dragContainer.reset()

    this.overlayRef.updatePositionStrategy(
      this.overlay.position()
        .global()
        .top((bbox.top - parent.top) + 'px')
        .left((bbox.left - parent.left) + 'px')
    );

    this.hasBeenMoved = true;
  }

  onAnimationComplete(event: AnimationEvent) {
    if (event.toState === 'void') {
      this.afterDispose.next(this.mapPin.pin.ID)
      this.overlayRef.dispose();
    }
  }

  containerClass = '';

  constructor(
    @Inject(OVERLAY_REF) public readonly overlayRef: OverlayRef,
    @Inject(MapOverlay) public overlay: Overlay,
    private dialog: SfngDialogService,
    private actionIndicator: ActionIndicatorService,
    private router: Router,
    private cdr: ChangeDetectorRef,
  ) { }

  ngOnInit(): void {
    this.showContent = true;
    this.cdr.markForCheck();
  }

  disposeOverlay() {
    this.showContent = false;
    this.cdr.markForCheck();
  }

  showExitConnections() {
    this.router.navigate(['/monitor'], {
      queryParams: {
        q: 'exit_node:' + this.mapPin.pin.ID
      }
    })
  }

  async copyNodeID() {
    await this.integration.writeToClipboard(this.mapPin?.pin.ID)
    this.actionIndicator.success("Copied to Clipboard")
  }
}
