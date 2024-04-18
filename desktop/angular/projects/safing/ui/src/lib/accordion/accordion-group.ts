import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ChangeDetectionStrategy, Component, Input, OnDestroy, TemplateRef } from '@angular/core';
import { Subscription } from 'rxjs';
import { SfngAccordionComponent } from './accordion';

@Component({
  selector: 'sfng-accordion-group',
  templateUrl: './accordion-group.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngAccordionGroupComponent implements OnDestroy {
  /** @private Currently registered accordion components */
  accordions: SfngAccordionComponent[] = [];

  /**
   * A template-ref to render as the header for each accordion-component.
   * Receives the accordion data as an $implicit context.
   */
  @Input()
  set headerTemplate(v: TemplateRef<any> | null) {
    this._headerTemplate = v;

    if (!!this.accordions.length) {
      this.accordions.forEach(a => {
        a.headerTemplate = v;
        a.cdr.markForCheck();
      })
    }
  }
  get headerTemplate() { return this._headerTemplate }
  private _headerTemplate: TemplateRef<any> | null = null;

  /** Whether or not one or more components can be expanded. */
  @Input()
  set singleMode(v: any) {
    this._singleMode = coerceBooleanProperty(v);
  }
  get singleMode() { return this._singleMode }
  private _singleMode = false;

  /** Whether or not the accordion is disabled and does not allow expanding */
  @Input()
  set disabled(v: any) {
    this._disabled = coerceBooleanProperty(v);
    if (this._disabled) {
      this.accordions.forEach(a => a.active = false);
    }
  }
  get disabled(): boolean { return this._disabled; }
  private _disabled = false;

  /** A list of subscriptions to the activeChange output of the registered accordion-components */
  private subscriptions: Subscription[] = [];

  /**
   * Registeres an accordion component to be handled together with this
   * accordion group.
   *
   * @param a The accordion component to register
   */
  register(a: SfngAccordionComponent) {
    this.accordions.push(a);

    // Tell the accordion-component about the default header-template.
    if (!a.headerTemplate) {
      a.headerTemplate = this.headerTemplate;
    }

    // Subscribe to the activeChange output of the registered
    // accordion and call toggle() for each event emitted.
    this.subscriptions.push(a.activeChange.subscribe(() => {
      if (this.disabled) {
        return;
      }

      this.toggle(a);
    }))
  }

  /**
   * Unregisters a accordion component
   *
   * @param a The accordion component to unregister
   */
  unregister(a: SfngAccordionComponent) {
    const index = this.accordions.indexOf(a);
    if (index === -1) return;
  
    const subscription = this.subscriptions[index];
  
    subscription.unsubscribe();
    this.accordions = this.accordions.splice(index, 1);
    this.subscriptions = this.subscriptions.splice(index, 1);
  }

  ngOnDestroy() {
    this.subscriptions.forEach(s => s.unsubscribe());
    this.subscriptions = [];
    this.accordions = [];
  }

  /**
   * Expand an accordion component and collaps all others if
   * single-mode is selected.
   *
   * @param a The accordion component to toggle.
   */
  private toggle(a: SfngAccordionComponent) {
    if (!a.active && this._singleMode) {
      this.accordions?.forEach(a => a.active = false);
    }

    a.active = !a.active;
  }

}
