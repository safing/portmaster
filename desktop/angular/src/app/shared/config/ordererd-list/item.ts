import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, EventEmitter, Input, OnInit, Output } from '@angular/core';

@Component({
  selector: 'app-ordered-list-item',
  templateUrl: './item.html',
  styleUrls: ['./item.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class OrderedListItemComponent implements OnInit {
  @Input()
  set readonly(v: any) {
    this._readonly = coerceBooleanProperty(v);
  }
  get readonly() {
    return this._readonly;
  }
  private _readonly = false;

  @Input()
  set value(v: string) {
    this._value = v;
    this._savedValue = v;
  }
  get value() {
    return this._value;
  }
  _value = '';

  private _savedValue = '';

  @Output()
  readonly valueChange = new EventEmitter<string>();

  @Output()
  readonly delete = new EventEmitter<void>();

  @Input()
  set edit(v: any) {
    this._edit = coerceBooleanProperty(v);
  }
  get edit() {
    return this._edit;
  }
  _edit = false;

  @Output()
  readonly editChange = new EventEmitter<boolean>();

  ngOnInit() {
    if (this._value === '' && this._savedValue === '') {
      this.edit = true;
    }
  }

  toggleEdit() {
    const wasEdit = this._edit;
    this._edit = !wasEdit;
    this.editChange.next(this._edit);

    if (!wasEdit) {
      return;
    }

    if (this._value !== this._savedValue) {
      this._value = this._value.trim()

      this.valueChange.next(this.value);
      this._savedValue = this._value;
    }
    this.changeDetectorRef.markForCheck();
  }

  reset() {
    if (this._edit) {
      if (this._value !== '' || this._savedValue !== '') {
        this._value = this._savedValue;
        this.changeDetectorRef.markForCheck();
        return;
      }
    }

    this.delete.next();
  }

  constructor(private changeDetectorRef: ChangeDetectorRef) { }
}
