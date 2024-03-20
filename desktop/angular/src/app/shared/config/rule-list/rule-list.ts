import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { CdkDragDrop, moveItemInArray } from '@angular/cdk/drag-drop';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, forwardRef, HostBinding, HostListener, Input, QueryList, ViewChildren } from '@angular/core';
import { ControlValueAccessor, NG_VALUE_ACCESSOR } from '@angular/forms';
import { SfngDialogService } from '@safing/ui';
import { RuleListItemComponent } from './list-item';

@Component({
  selector: 'app-rule-list',
  templateUrl: './rule-list.html',
  styleUrls: ['./rule-list.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
  providers: [
    {
      provide: NG_VALUE_ACCESSOR,
      useExisting: forwardRef(() => RuleListComponent),
      multi: true,
    }
  ],
})
export class RuleListComponent implements ControlValueAccessor {
  /** Add the host element into the tab-sequence */
  @HostBinding('tabindex')
  readonly tabindex = 0;

  @ViewChildren(RuleListItemComponent)
  renderedRules!: QueryList<RuleListItemComponent>;

  /** A list of selected rule indexes */
  selectedItems: number[] = [];

  /**
   * @private
   * Mark the component as dirty by calling the onTouch callback of the control-value accessor
   */
  @HostListener('blur')
  onBlur() {
    this.onTouch();
  }

  @Input()
  symbolMap = {
    '+': 'Allow',
    '-': 'Block',
  }

  /**
   * Whether or not the component should be displayed as read-only.
   */
  @Input()
  set readonly(v: any) {
    this._readonly = coerceBooleanProperty(v);
  }
  get readonly() {
    return this._readonly;
  }
  private _readonly = false;

  /**
   * @private
   * The actual rule entries. Displayed as RuleListItemComponent.
   */
  entries: string[] = [];

  constructor(
    private changeDetector: ChangeDetectorRef,
    private dialog: SfngDialogService
  ) { }

  /**
   * @private
   * Update the value of a rule-list entry. Used as a callback function
   * for the valueChange output of the RuleListItemComponent.
   *
   * @param index The index of the rule list entry to update
   * @param newValue The new value of the rule
   */
  updateValue(index: number, newValue: string) {
    // we need create a copy of the actual value as
    // the parent component might still have a reference
    // to the current values.
    this.entries = [
      ...this.entries,
    ];
    this.entries[index] = newValue;

    // tell the control that we have a new value
    this.onChange(this.entries);
  }

  /**
   * @private
   * Delete a rule list entry.
   *
   * @param index The index of the rule list entry to delete
   */
  deleteEntry(index: number) {
    this.entries = [...this.entries];
    this.entries.splice(index, 1);
    this.onChange(this.entries);
  }

  /**
   * @private
   * Add a new, empty rule list entry at the end of the
   * list.
   *
   * This is a no-op if there's already an empty item
   * available.
   */
  addEntry() {
    // if there's already one empty entry abort
    if (this.entries.some(e => e.trim() === '')) {
      return;
    }

    this.entries = [...this.entries];
    this.entries.push('');
  }

  /**
   * Set a new value for the rule list. This is the
   * only way to configure the existing entries and is
   * used by the control-value-accessor and ngModel.
   *
   * @param value The new value set via [ngModel]
   */
  writeValue(value: string[]) {
    this.entries = value;

    this.changeDetector.markForCheck();
  }

  /** Toggles selection of a rule item */
  selectItem(index: number, selected: boolean) {
    if (selected && !this.selectedItems.includes(index)) {
      this.selectedItems = [
        ...this.selectedItems,
        index,
      ]

      return;
    }

    if (!selected && this.selectedItems.includes(index)) {
      this.selectedItems = this.selectedItems.filter(idx => idx !== index)

      return;
    }
  }

  /** Removes all selected items after displaying a confirmation dialog. */
  removeSelectedItems() {
    this.dialog.confirm({
      buttons: [
        {
          id: 'abort',
          text: 'Cancel',
          class: 'outline'
        },
        {
          id: 'delete',
          text: 'Delete Rules',
          class: 'danger'
        }
      ],
      canCancel: true,
      caption: 'Caution',
      header: 'Rule Deletion',
      message: 'Do you want to delete the selected rules'
    })
      .onAction('delete', () => {
        this.entries = this.entries.filter((_, idx: number) => !this.selectedItems.includes(idx))
        this.abortSelection();
        this.onChange(this.entries);
      })

  }

  /** Aborts the current selection */
  abortSelection() {
    this.selectedItems.forEach(itemIdx => this.renderedRules.get(itemIdx)?.toggleSelection())
    this.selectedItems = [];
  }

  /** @private onChange callback registered by ngModel and form controls */
  onChange = (_: string[]): void => { };

  /** Registers the onChange callback and required for the ControlValueAccessor interface */
  registerOnChange(fn: (value: string[]) => void) {
    this.onChange = fn;
  }

  /** @private onTouch callback registered by ngModel and form controls */
  onTouch = (): void => { };

  /** Registers the onChange callback and required for the ControlValueAccessor interface */
  registerOnTouched(fn: () => void) {
    this.onTouch = fn;
  }

  /**
   * @private
   * Used as a callback for the @angular/cdk drop component
   * and used to update the actual order of the entries.
   *
   * @param event The drop-event
   */
  drop(event: CdkDragDrop<string[]>) {
    if (this._readonly) {
      return;
    }

    // create a copy of the array
    this.entries = [...this.entries];
    moveItemInArray(this.entries, event.previousIndex, event.currentIndex);

    this.changeDetector.markForCheck();
    this.onChange(this.entries);
  }

  /** @private TrackByFunction for entries */
  trackBy(idx: number, value: string) {
    return `${value}`;
  }
}
