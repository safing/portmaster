import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { DOCUMENT } from '@angular/common';
import { AfterViewChecked, ChangeDetectionStrategy, ChangeDetectorRef, Component, ElementRef, EventEmitter, forwardRef, Inject, Input, Output, ViewChild } from '@angular/core';
import { AbstractControl, ControlValueAccessor, NgModel, NG_VALIDATORS, NG_VALUE_ACCESSOR, ValidationErrors, Validator } from '@angular/forms';
import { BaseSetting, ExternalOptionHint, OptionType, parseSupportedValues, SettingValueType, WellKnown } from '@safing/portmaster-api';

@Component({
  selector: 'app-basic-setting',
  templateUrl: './basic-setting.html',
  styleUrls: ['./basic-setting.scss'],
  providers: [
    {
      provide: NG_VALUE_ACCESSOR,
      multi: true,
      useExisting: forwardRef(() => BasicSettingComponent),
    },
    {
      provide: NG_VALIDATORS,
      multi: true,
      useExisting: forwardRef(() => BasicSettingComponent),
    }
  ],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class BasicSettingComponent<S extends BaseSetting<any, any>> implements ControlValueAccessor, Validator, AfterViewChecked {
  /** @private template-access to all external option hits */
  readonly optionHints = ExternalOptionHint;

  /** @private template-access to parseSupportedValues */
  readonly parseSupportedValues = parseSupportedValues;

  @ViewChild('suffixElement', { static: false, read: ElementRef })
  suffixElement?: ElementRef<HTMLSpanElement>;

  /** Cached canvas element used by getTextWidth */
  private cachedCanvas?: HTMLCanvasElement;

  /** Returns the value of external-option hint annotation */
  externalOptType(opt: S): ExternalOptionHint | null {
    return opt.Annotations?.["safing/portbase:ui:display-hint"] || null;
  }

  /** Whether or not the input should be currently disabled. */
  @Input()
  set disabled(v: any) {
    const disabled = coerceBooleanProperty(v);
    this.setDisabledState(disabled);
  }
  get disabled() {
    return this._disabled;
  }

  /** The setting to display */
  @Input()
  setting: S | null = null;

  /** Emits when the user activates focus on this component */
  @Output()
  blured = new EventEmitter<void>();

  /** @private The ngModel in our view used to display the value */
  @ViewChild(NgModel)
  model: NgModel | null = null;

  /** The unit of the setting */
  get unit() {
    if (!this.setting) {
      return '';
    }
    return this.setting.Annotations[WellKnown.Unit] || '';
  }

  /**
   * Holds the value as it is presented to the user.
   * That is, a JSON encoded object or array is dumped as a
   * JSON string. Strings, numbers and booleans are presented
   * as they are.
   */
  _value: string | number | boolean = "";

  /**
   * Describes the type of the original settings value
   * as passed to writeValue().
   * This may be anything that can be returned from `typeof v`.
   * If set to "string", "number" or "boolean" then _value is emitted
   * as it is.
   * If it's set anything else (like "object") than _value is JSON.parse`d
   * before being emitted.
   */
  _type: string = '';

  /* Returns true if the current _type and _value is managed as JSON */
  get isJSON(): boolean {
    return this._type !== 'string'
      && this._type !== 'number'
      && this._type !== 'boolean'
  }

  /*
   * _onChange is set using registerOnChange by @angular/forms
   * and satisfies the ControlValueAccessor.
   */
  private _onChange: (_: SettingValueType<S>) => void = () => { };

  /* _onTouch is set using registerOnTouched by @angular/forms
   * and satisfies the ControlValueAccessor.
   */
  private _onTouch: () => void = () => { };

  private _onValidatorChange: () => void = () => { };

  /* Whether or not the input field is disabled. Set by setDisabledState
   * from @angular/forms
   */
  _disabled: boolean = false;
  private _valid: boolean = true;

  // We are using ChangeDetectionStrategy.OnPush so angular does not
  // update ourself when writeValue or setDisabledState is called.
  // Using the changeDetectorRef we can take care of that ourself.
  constructor(
    @Inject(DOCUMENT) private document: Document,
    private _changeDetectorRef: ChangeDetectorRef
  ) { }

  ngAfterViewChecked() {
    // update the suffix position everytime angular has
    // checked our view for changes.
    this.updateUnitSuffixPosition();
  }

  /**
   * Sets the user-presented value and emits a change.
   * Used by our view. Not meant to be used from outside!
   * Use writeValue instead.
   * @private
   *
   * @param value The value to set
   */
  setInternalValue(value: string | number | boolean) {
    let toEmit: any = value;
    try {
      if (!this.isJSON) {
        toEmit = value;
      } else {
        toEmit = JSON.parse(value as string);
      }
    } catch (err) {
      this._valid = false;
      this._onValidatorChange();
      return;
    }

    this._valid = true;
    this._value = value;
    this._onChange(toEmit);
    this.updateUnitSuffixPosition();
  }

  /**
   * Updates the position of the value's unit suffix element
   */
  private updateUnitSuffixPosition() {
    if (!!this.unit && !!this.suffixElement) {
      const input = this.suffixElement.nativeElement.previousSibling! as HTMLInputElement;
      const style = window.getComputedStyle(input);
      let paddingleft = parseInt(style.paddingLeft.slice(0, -2))
      // we need to use `input.value` instead of `value` as we need to
      // get preceding zeros of the number input as well, while still
      // using the value as a fallback.
      let value = input.value || (this._value as string);
      const width = this.getTextWidth(value, style.font) + paddingleft;
      this.suffixElement.nativeElement.style.left = `${width}px`;
    }
  }

  /**
   * Validates if "value" matches the settings requirements.
   * It satisfies the NG_VALIDATORS interface and validates the
   * value for THIS component.
   *
   * @param param0 The AbstractControl to validate
   */
  validate({ value }: AbstractControl): ValidationErrors | null {
    if (!this._valid) {
      return {
        jsonParseError: true
      }
    }

    if (this._type === 'string' || value === null) {
      if (!!this.setting?.DefaultValue && !value) {
        return {
          required: true,
        }
      }
    }

    if (!!this.setting?.ValidationRegex) {
      const re = new RegExp(this.setting.ValidationRegex);

      if (!this.isJSON) {
        if (!re.test(`${value}`)) {
          return {
            pattern: `"${value}"`
          }
        }
      } else {
        if (!Array.isArray(value)) {
          return {
            invalidType: true
          }
        }
        const invalidLines = value.filter(v => !re.test(v));
        if (invalidLines.length) {
          return {
            pattern: invalidLines
          }
        }
      }
    }

    return null;
  }

  /**
   * Writes a new value and satisfies the ControlValueAccessor
   *
   * @param v The new value to write
   */
  writeValue(v: SettingValueType<S>) {
    // the following is a super ugly work-around for the migration
    // from security-settings to booleans.
    //
    // In order to not mess and hide an actual portmaster issue
    // we only convert v to a boolean if it's a number value and marked as a security setting.
    // In all other cases we don't mangle it.
    //
    // TODO(ppacher): Remove in v1.8?
    // BOM
    if (this.setting?.OptType === OptionType.Bool && this.setting?.Annotations[WellKnown.DisplayHint] === ExternalOptionHint.SecurityLevel) {
      if (typeof v === 'number') {
        (v as any) = v === 7;
      }
    }
    // EOM

    let t = typeof v;
    this._type = t;

    if (this.isJSON) {
      this._value = JSON.stringify(v, undefined, 2);
    } else {
      this._value = v;
    }

    this.updateUnitSuffixPosition();
    this._changeDetectorRef.markForCheck();
  }

  registerOnValidatorChange(fn: () => void) {
    this._onValidatorChange = fn;
  }

  /**
   * Registers the onChange function requred by the
   * ControlValueAccessor
   *
   * @param fn The fn to register
   */
  registerOnChange(fn: (_: SettingValueType<S>) => void) {
    this._onChange = fn;
  }

  /**
   * @private
   * Called when the input-component used for the setting is touched/focused.
   */
  touched() {
    this._onTouch();
    this.blured.next();
  }

  /**
   * Registers the onTouch function requred by the
   * ControlValueAccessor
   *
   * @param fn The fn to register
   */
  registerOnTouched(fn: () => void) {
    this._onTouch = fn;
  }

  /**
   * Enable or disable the component. Required for the
   * ControlValueAccessor.
   *
   * @param disable Whether or not the component is disabled
   */
  setDisabledState(disable: boolean) {
    this._disabled = disable;
    this._changeDetectorRef.markForCheck();
  }

  /**
   * @private
   * Returns the number of lines in value. If value is not
   * a string 1 is returned.
   */
  lineCount(value: string | number | boolean) {
    if (typeof value === 'string') {
      return value.split('\n').length
    }
    return 1
  }

  /**
   * Calculates the amount of pixel a text requires when being rendered.
   * It uses canvas.measureText on a dummy (no attached) element
   *
   * @param text The text that would be rendered
   * @param font The CSS font descriptor that would be used for the text
   */
  private getTextWidth(text: string, font: string): number {
    let canvas = this.cachedCanvas || this.document.createElement('canvas');
    this.cachedCanvas = canvas;

    let context = canvas.getContext("2d")!;
    context.font = font;
    let metrics = context.measureText(text);
    return metrics.width;
  }
}
