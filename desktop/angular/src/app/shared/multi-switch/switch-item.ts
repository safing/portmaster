import { Component, ChangeDetectionStrategy, Input, isDevMode, OnInit, HostBinding, Output, EventEmitter, HostListener, ElementRef, ChangeDetectorRef } from '@angular/core';
import { coerceBooleanProperty } from '@angular/cdk/coercion';

@Component({
  selector: 'app-switch-item',
  template: '<ng-content></ng-content>',
  styleUrls: ['./switch-item.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SwitchItemComponent<T> implements OnInit {
  @Input()
  id: T | null = null;

  @Input()
  group = '';

  @Output()
  clicked = new EventEmitter<MouseEvent>();

  @HostListener('click', ['$event'])
  onClick(e: MouseEvent) {
    this.clicked.next(e);
  }

  @Input()
  borderColorActive: string = 'var(--info-green)';

  @Input()
  borderColorInactive: string = 'var(--button-light)';

  @HostBinding('style.border-color')
  get borderColor() {
    if (this.selected) {
      return this.borderColorActive;
    }
    return this.borderColorInactive;
  }

  @Input()
  @HostBinding('class.disabled')
  set disabled(v: any) {
    this._disabled = coerceBooleanProperty(v);
  }
  get disabled() {
    return this._disabled;
  }
  private _disabled = false;

  @Input()
  @HostBinding('class.selected')
  set selected(v: any) {
    const selected = coerceBooleanProperty(v);
    if (selected !== this._selected) {
      this._selected = selected;
      this.selectedChange.next(selected);
    }
  }
  get selected() {
    return this._selected;
  }
  private _selected = false;

  getLabel() {
    return this.elementRef.nativeElement.innerText;
  }

  @Output()
  selectedChange = new EventEmitter<boolean>();

  ngOnInit() {
    if (this.id === null && isDevMode()) {
      throw new Error(`SwitchItemComponent must have an ID`);
    }
  }

  constructor(
    public readonly elementRef: ElementRef,
    public readonly changeDetectorRef: ChangeDetectorRef,
  ) { }
}
