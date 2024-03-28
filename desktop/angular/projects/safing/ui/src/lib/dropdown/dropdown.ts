import { coerceBooleanProperty, coerceCssPixelValue, coerceNumberProperty } from "@angular/cdk/coercion";
import { CdkOverlayOrigin, ConnectedPosition, ScrollStrategy, ScrollStrategyOptions } from "@angular/cdk/overlay";
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, ElementRef, EventEmitter, Input, OnInit, Output, Renderer2, TemplateRef, ViewChild } from "@angular/core";
import { fadeInAnimation, fadeOutAnimation } from '../animations';

@Component({
  selector: 'sfng-dropdown',
  exportAs: 'sfngDropdown',
  templateUrl: './dropdown.html',
  styles: [
    `
    :host {
      display: block;
    }
    `
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [fadeInAnimation, fadeOutAnimation],
})
export class SfngDropdownComponent implements OnInit {
  /** The trigger origin used to open the drop-down */
  @ViewChild('trigger', { read: CdkOverlayOrigin })
  trigger: CdkOverlayOrigin | null = null;

  /**
   * The button/drop-down label. Only when not using
   * {@Link SfngDropdown.externalTrigger}
   */
  @Input()
  label: string = '';

  /** The trigger template to use when {@Link SfngDropdown.externalTrigger} */
  @Input()
  triggerTemplate: TemplateRef<any> | null = null;

  /** Set to true to provide an external dropdown trigger template using {@Link SfngDropdown.triggerTemplate} */
  @Input()
  set externalTrigger(v: any) {
    this._externalTrigger = coerceBooleanProperty(v)
  }
  get externalTrigger() {
    return this._externalTrigger;
  }
  private _externalTrigger = false;

  /** A list of classes to apply to the overlay element */
  @Input()
  overlayClass: string = '';

  /** Whether or not the drop-down is disabled. */
  @Input()
  set disabled(v: any) {
    this._disabled = coerceBooleanProperty(v)
  }
  get disabled() {
    return this._disabled;
  }
  private _disabled = false;

  /** The Y-offset of the drop-down overlay */
  @Input()
  set offsetY(v: any) {
    this._offsetY = coerceNumberProperty(v);
  }
  get offsetY() { return this._offsetY }
  private _offsetY = 4;

  /** The X-offset of the drop-down overlay */
  @Input()
  set offsetX(v: any) {
    this._offsetX = coerceNumberProperty(v);
  }
  get offsetX() { return this._offsetX }
  private _offsetX = 0;

  /** The scrollStrategy of the drop-down */
  @Input()
  scrollStrategy!: ScrollStrategy;

  /** Whether or not the pop-over is currently shown. Do not modify this directly */
  isOpen = false;

  /** The minimum width of the drop-down */
  @Input()
  set minWidth(val: any) {
    this._minWidth = coerceCssPixelValue(val)
  }
  get minWidth() { return this._minWidth }
  private _minWidth: string | number = 0;

  /** The maximum width of the drop-down */
  @Input()
  set maxWidth(val: any) {
    this._maxWidth = coerceCssPixelValue(val)
  }
  get maxWidth() { return this._maxWidth }
  private _maxWidth: string | number | null = null;

  /** The minimum height of the drop-down */
  @Input()
  set minHeight(val: any) {
    this._minHeight = coerceCssPixelValue(val)
  }
  get minHeight() { return this._minHeight }
  private _minHeight: string | number | null = null;

  /** The maximum width of the drop-down */
  @Input()
  set maxHeight(val: any) {
    this._maxHeight = coerceCssPixelValue(val)
  }
  get maxHeight() { return this._maxHeight }
  private _maxHeight: string | number | null = null;

  /** Emits whenever the drop-down is opened */
  @Output()
  opened = new EventEmitter<void>();

  /** Emits whenever the drop-down is closed. */
  @Output()
  closed = new EventEmitter<void>();

  @Input()
  positions: ConnectedPosition[] = [
    {
      originX: 'end',
      originY: 'bottom',
      overlayX: 'end',
      overlayY: 'top',
    },
    {
      originX: 'end',
      originY: 'top',
      overlayX: 'end',
      overlayY: 'bottom',
    },
    {
      originX: 'end',
      originY: 'bottom',
      overlayX: 'start',
      overlayY: 'bottom',
    },
  ]

  constructor(
    public readonly elementRef: ElementRef,
    private changeDetectorRef: ChangeDetectorRef,
    private renderer: Renderer2,
    private scrollOptions: ScrollStrategyOptions,
  ) {
  }

  ngOnInit() {
    this.scrollStrategy = this.scrollStrategy || this.scrollOptions.close();
  }

  onOutsideClick(event: MouseEvent) {
    if (!!this.trigger) {
      const triggerEl = this.trigger.elementRef.nativeElement;

      let node = event.target;
      while (!!node) {
        if (node === triggerEl) {
          return;
        }
        node = this.renderer.parentNode(node);
      }
    }

    this.close();
  }

  onOverlayClosed() {
    this.closed.next();
  }

  close() {
    if (!this.isOpen) {
      return;
    }

    this.isOpen = false;
    this.changeDetectorRef.markForCheck();
  }

  toggle(t: CdkOverlayOrigin | null = this.trigger) {
    if (this.isOpen) {
      this.close();

      return;
    }

    this.show(t);
  }

  show(t: CdkOverlayOrigin | null = this.trigger) {
    if (t === null) {
      return;
    }

    if (this.isOpen || this._disabled) {
      return;
    }

    if (!!t) {
      this.trigger = t;
      const rect = (this.trigger.elementRef.nativeElement as HTMLElement).getBoundingClientRect()

      this.minWidth = rect ? rect.width : this.trigger.elementRef.nativeElement.offsetWidth;

    }
    this.isOpen = true;
    this.opened.next();
    this.changeDetectorRef.markForCheck();
  }
}
