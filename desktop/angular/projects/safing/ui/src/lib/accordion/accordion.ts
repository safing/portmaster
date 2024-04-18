import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, EventEmitter, HostBinding, Input, OnDestroy, OnInit, Optional, Output, TemplateRef, TrackByFunction } from '@angular/core';
import { fadeInAnimation, fadeOutAnimation } from '../animations';
import { SfngAccordionGroupComponent } from './accordion-group';

@Component({
  selector: 'sfng-accordion',
  templateUrl: './accordion.html',
  exportAs: 'sfngAccordion',
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    fadeInAnimation,
    fadeOutAnimation
  ]
})
export class SfngAccordionComponent<T = any> implements OnInit, OnDestroy {
  /** @deprecated in favor of [data] */
  @Input()
  title: string = '';

  /** A reference to the component provided via the template context */
  component = this;

  /**
   * The data the accordion component is used for. This is passed as an $implicit context
   * to the header template.
   */
  @Input()
  data: T | undefined = undefined;

  @Input()
  trackBy: TrackByFunction<T | null> = (_, c) => c

  /** Whether or not the accordion component starts active. */
  @Input()
  set active(v: any) {
    this._active = coerceBooleanProperty(v);
  }
  get active() {
    return this._active;
  }
  private _active: boolean = false;

  /** Emits whenever the active value changes. Supports two-way bindings. */
  @Output()
  activeChange = new EventEmitter<boolean>();

  /**
   * The header-template to render for this component. If null, the default template from
   * the parent accordion-group will be used.
   */
  @Input()
  headerTemplate: TemplateRef<any> | null = null;

  @HostBinding('class.active')
  /** @private Whether or not the accordion should have the 'active' class */
  get activeClass(): string {
    return this.active;
  }

  ngOnInit(): void {
    // register at our parent group-component (if any).
    this.group?.register(this);
  }

  ngOnDestroy(): void {
    this.group?.unregister(this);
  }

  /**
   * Toggle the active-state of the accordion-component.
   *
   * @param event The mouse event.
   */
  toggle(event?: Event) {
    if (!!this.group && this.group.disabled) {
      return;
    }

    event?.preventDefault();
    this.activeChange.emit(!this.active);
  }

  constructor(
    public cdr: ChangeDetectorRef,
    @Optional() public group: SfngAccordionGroupComponent,
  ) { }
}
