import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, EventEmitter, HostBinding, Input, OnInit, Output } from '@angular/core';
import { fadeInAnimation, fadeOutAnimation } from '../../animations';

@Component({
  selector: 'app-rule-list-item',
  templateUrl: 'list-item.html',
  styleUrls: ['list-item.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    fadeInAnimation,
    fadeOutAnimation
  ]
})
export class RuleListItemComponent implements OnInit {
  /** The host element is going to fade in/out */
  @HostBinding('@fadeIn')
  @HostBinding('@fadeOut')
  readonly animation = true;

  @Input()
  symbolMap: { [key: string]: string } = {}

  /**
   * The current value (rule) displayed by this component.
   * Supports two-way bindings.
   */
  @Input()
  set value(v: string) {
    this.updateValue(v);
    this._savedValue = this._value;
  }
  private _value = '';

  /** The last actually saved value of this rule. Required for resets */
  private _savedValue = '';

  /**
   * Emits whenever the rule value changes.
   * Supports two-way-bindings on ([value])
   */
  @Output()
  valueChange = new EventEmitter<string>();

  /** Whether or not the rule list item is selected */
  @Input()
  set selected(v: any) {
    this._selected = coerceBooleanProperty(v)
  }
  get selected() {
    return this._selected;
  }
  private _selected = false;

  @Output()
  selectedChange = new EventEmitter<boolean>();

  /**
   * Whether or not the component is in edit mode.
   * Supports two-way-bindings on ([edit])
   */
  @Input()
  set edit(v: any) {
    this._edit = coerceBooleanProperty(v);
  }
  get edit() {
    return this._edit;
  }
  private _edit: boolean = false;

  /**
   * Emits whenever the component switch to or away from edit
   * mode.
   * Supports two-way-bindings on ([edit])
   */
  @Output()
  editChange = new EventEmitter<boolean>();

  /**
   * Whether or not the component should be in read-only mode.
   */
  @Input()
  set readonly(v: any) {
    this._readonly = coerceBooleanProperty(v);
  }
  get readonly() {
    return this._readonly;
  }
  private _readonly: boolean = false;

  /**
   * Emits when the user presses the delete button of
   * this rule component.
   */
  @Output()
  delete = new EventEmitter<void>();

  /** @private Whether or not this rule is a "Allow" rule - we default to allow since this is what most rules are used for */
  isAllow = true;

  /** @private Whether or not this rule is a "Deny" rule */
  isBlock = false;

  /** @private the actually displayed rule value (without the verdict) */
  display = '';

  /** @private the character representation of the current verdict */
  get currentAction() {
    if (this.isBlock) {
      return '-';
    }
    if (this.isAllow) {
      return '+';
    }
    return '';
  }

  constructor(private cdr: ChangeDetectorRef) { }

  ngOnInit() {
    // new entries always start in edit mode
    if (!this.isAllow && !this.isBlock) {
      this._edit = true;
    }
  }

  /**
   * @private
   * Toggle between edit and view mode. When switching from
   * edit to view mode, the current value is emitted to the
   * parent element in case it has been changed.
   */
  toggleEdit() {
    if (this._edit) {
      // do nothing if the rule is obviously invalid (no verdict or value).
      if (this.display === '' || !(this.isAllow || this.isBlock)) {
        return;
      }

      if (this._value !== this._savedValue) {
        this.valueChange.next(this._value);
      }
    }

    this._edit = !this._edit;
    this.editChange.next(this._edit);
  }

  toggleSelection() {
    this.selected = !this.selected;
    this.selectedChange.next(this.selected);

    this.cdr.markForCheck();
  }

  /**
   * @private
   * Sets the new rule action. Used as a callback in the drop-down.
   *
   * @param action The new action
   */
  setAction(action: '+' | '-') {
    this.updateValue(`${action} ${this.display}`);
  }

  /**
   * @private
   * Update the actual value of the rule.
   *
   * @param entity The new rule value
   */
  setEntity(entity: string) {
    const action = this.isAllow ? '+' : '-';
    this.updateValue(`${action} ${entity}`);
  }

  /**
   * @private
   *
   * Reset the value to it's previously saved value if it was changed.
   * If the value is unchanged a reset counts as a delete and triggers
   * on our delete output.
   */
  reset() {
    if (this._edit) {
      // if the user did not change anything we can immediately
      // delete it.
      if (this._savedValue !== '') {
        this.value = this._savedValue;
        this._edit = false;
        return;
      }
    }

    this.delete.next();
  }

  /**
   * Updates our internal states to correctly display the rule.
   *
   * @param v The actual rule value
   */
  private updateValue(v: string) {
    this._value = v.trim();
    switch (this._value[0]) {
      case '+':
        this.isAllow = true;
        this.isBlock = false;
        break;
      case '-':
        this.isAllow = false;
        this.isBlock = true;
        break;
      default:
        // not yet set
        this.isBlock = this.isAllow = false;
    }

    this.display = this._value.slice(1).trim();
  }
}
