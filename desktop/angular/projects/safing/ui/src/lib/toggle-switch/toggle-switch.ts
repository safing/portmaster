import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, forwardRef, HostListener } from '@angular/core';
import { ControlValueAccessor, NG_VALUE_ACCESSOR } from '@angular/forms';

@Component({
  selector: 'sfng-toggle',
  templateUrl: './toggle-switch.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  providers: [
    {
      provide: NG_VALUE_ACCESSOR,
      useExisting: forwardRef(() => SfngToggleSwitchComponent),
      multi: true,
    }
  ]
})
export class SfngToggleSwitchComponent implements ControlValueAccessor {
  @HostListener('blur')
  onBlur() {
    this.onTouch();
  }

  set disabled(v: any) {
    this.setDisabledState(coerceBooleanProperty(v))
  }
  get disabled() {
    return this._disabled;
  }
  private _disabled = false;

  value: boolean = false;

  constructor(private _changeDetector: ChangeDetectorRef) { }

  setDisabledState(isDisabled: boolean) {
    this._disabled = isDisabled;
    this._changeDetector.markForCheck();
  }

  onValueChange(value: boolean) {
    this.value = value;
    this.onChange(this.value);
  }

  writeValue(value: boolean) {
    this.value = value;
    this._changeDetector.markForCheck();
  }

  onChange = (_: any): void => { };
  registerOnChange(fn: (value: any) => void) {
    this.onChange = fn;
  }

  onTouch = (): void => { };
  registerOnTouched(fn: () => void) {
    this.onTouch = fn;
  }
}
