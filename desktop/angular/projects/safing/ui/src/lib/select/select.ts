import { ListKeyManager, ListKeyManagerOption } from '@angular/cdk/a11y';
import { coerceBooleanProperty, coerceNumberProperty } from '@angular/cdk/coercion';
import { AfterViewInit, ChangeDetectionStrategy, ChangeDetectorRef, Component, ContentChildren, DestroyRef, Directive, ElementRef, EventEmitter, HostBinding, HostListener, Input, OnDestroy, Output, QueryList, TemplateRef, ViewChild, ViewChildren, forwardRef, inject } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ControlValueAccessor, NG_VALUE_ACCESSOR } from '@angular/forms';
import { BehaviorSubject, combineLatest } from 'rxjs';
import { startWith } from 'rxjs/operators';
import { SfngDropdownComponent } from '../dropdown';
import { SelectOption, SfngSelectValueDirective } from './item';


export type SelectModes = 'single' | 'multi';

type ModeInput = {
  mode: SelectModes;
}

type SelectValue<T, S extends ModeInput> = S['mode'] extends 'single' ? T : T[];

export type SortByFunc = (a: SelectOption, b: SelectOption) => number;

export type SelectDisplayMode = 'dropdown' | 'inline';

@Directive({
  selector: '[sfngSelectRenderedListItem]'
})
export class SfngSelectRenderedItemDirective implements ListKeyManagerOption {
  @Input('sfngSelectRenderedListItem')
  option: SelectOption | null = null;

  getLabel() {
    return this.option?.label || '';
  }

  get disabled() {
    return this.option?.disabled || false;
  }

  @HostBinding('class.bg-gray-300')
  set focused(v: boolean) {
    this._focused = v;
  }
  get focused() { return this._focused }
  private _focused = false;

  constructor(public readonly elementRef: ElementRef) { }
}

@Component({
  selector: 'sfng-select',
  templateUrl: './select.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  providers: [
    {
      provide: NG_VALUE_ACCESSOR,
      useExisting: forwardRef(() => SfngSelectComponent),
      multi: true,
    },
  ]
})
export class SfngSelectComponent<T> implements AfterViewInit, ControlValueAccessor, OnDestroy {
  /** emits the search text entered by the user */
  private search$ = new BehaviorSubject('');

  /** emits and completes when the component is destroyed. */
  private destroyRef = inject(DestroyRef);

  /** the key manager used for keyboard support */
  private keyManager!: ListKeyManager<SfngSelectRenderedItemDirective>;

  @ViewChild(SfngDropdownComponent, { static: false })
  dropdown: SfngDropdownComponent | null = null;

  /** A reference to the cdk-scrollable directive that's placed on the item list */
  @ViewChild('scrollable', { read: ElementRef })
  scrollableList?: ElementRef;

  @ContentChildren(SfngSelectValueDirective)
  userProvidedItems!: QueryList<SfngSelectValueDirective>;

  @ViewChildren('renderedItem', { read: SfngSelectRenderedItemDirective })
  renderedItems!: QueryList<SfngSelectRenderedItemDirective>;

  /** A list of all items available in the select box including dynamic ones. */
  allItems: SelectOption[] = []

  /** The acutally rendered list of items after applying search and item threshold */
  items: SelectOption[] = [];

  @Input()
  @HostBinding('attr.tabindex')
  readonly tabindex = 0;

  @HostBinding('attr.role')
  readonly role = 'listbox';

  value?: SelectValue<T, this>;

  /** A list of currently selected items */
  currentItems: SelectOption[] = [];

  /** The current search text. Used by ngModel */
  searchText = '';

  /** Whether or not the select operates in "single" or "multi" mode */
  @Input()
  mode: SelectModes = 'single';

  @Input()
  displayMode: SelectDisplayMode = 'dropdown';

  /** The placehodler to show when nothing is selected */
  @Input()
  placeholder = 'Select'

  /** The type of item to show in multi mode when more than one value is selected */
  @Input()
  itemName = '';

  /** The maximum number of items to render. */
  @Input()
  set itemLimit(v: any) {
    this._maxItemLimit = coerceNumberProperty(v)
  }
  get itemLimit(): number { return this._maxItemLimit }
  private _maxItemLimit = Infinity;

  /** The placeholder text for the search bar */
  @Input()
  searchPlaceholder = '';

  /** Whether or not the search bar is visible */
  @Input()
  set allowSearch(v: any) {
    this._allowSearch = coerceBooleanProperty(v);
  }
  get allowSearch(): boolean {
    return this._allowSearch;
  }
  private _allowSearch = false;

  /** The minimum number of items required for the search bar to be visible */
  @Input()
  set searchItemThreshold(v: any) {
    this._searchItemThreshold = coerceNumberProperty(v);
  }
  get searchItemThreshold(): number {
    return this._searchItemThreshold;
  }
  private _searchItemThreshold = 0;

  /**
   * Whether or not the select should be disabled when not options
   * are available.
   */
  @Input()
  set disableWhenEmpty(v: any) {
    this._disableWhenEmpty = coerceBooleanProperty(v);
  }
  get disableWhenEmpty() {
    return this._disableWhenEmpty;
  }
  private _disableWhenEmpty = false;

  /** Whether or not the select component will add options for dynamic values as well. */
  @Input()
  set dynamicValues(v: any) {
    this._dynamicValues = coerceBooleanProperty(v);
  }
  get dynamicValues() {
    return this._dynamicValues
  }
  private _dynamicValues = false;

  /** An optional template to use for dynamic values. */
  @Input()
  dynamicValueTemplate?: TemplateRef<any>;

  /** The minimum-width of the drop-down. See {@link SfngDropdownComponent.minWidth} */
  @Input()
  minWidth: any;

  /** The minimum-width of the drop-down. See {@link SfngDropdownComponent.minHeight} */
  @Input()
  minHeight: any;

  /** Whether or not selected items should be sorted to the top */
  @Input()
  set sortValues(v: any) {
    this._sortValues = coerceBooleanProperty(v);
  }
  get sortValues() {
    if (this._sortValues === null) {
      return this.mode === 'multi';
    }
    return this._sortValues;
  }
  private _sortValues: boolean | null = null;

  /** The sort function to use. Defaults to sort by label/value */
  @Input()
  sortBy: SortByFunc = (a: SelectOption, b: SelectOption) => {
    if ((a.label || a.value) < (b.label || b.value)) {
      return 1;
    }
    if ((a.label || a.value) > (b.label || b.value)) {
      return -1;
    }

    return 0;
  }

  @Input()
  set disabled(v: any) {
    const disabled = coerceBooleanProperty(v);
    this.setDisabledState(disabled);
  }
  get disabled() {
    return this._disabled;
  }
  private _disabled: boolean = false;

  @HostListener('keydown.enter', ['$event'])
  @HostListener('keydown.space', ['$event'])
  onEnter(event: Event) {
    if (!this.dropdown?.isOpen) {
      this.dropdown?.toggle()

      event.preventDefault();
      event.stopPropagation();

      return;
    }

    if (this.keyManager.activeItem !== null && !!this.keyManager.activeItem?.option) {
      this.selectItem(this.keyManager.activeItem.option)

      event.preventDefault();
      event.stopPropagation();

      return;
    }
  }

  @HostListener('keydown', ['$event'])
  onKeyDown(event: KeyboardEvent) {
    this.keyManager.onKeydown(event);
  }

  @Output()
  closed = new EventEmitter<void>();

  @Output()
  opened = new EventEmitter<void>();

  trackItem(_: number, item: SelectOption) {
    return item.value;
  }

  setDisabledState(disabled: boolean) {
    this._disabled = disabled;
    this.cdr.markForCheck();
  }

  constructor(private cdr: ChangeDetectorRef) { }

  ngAfterViewInit(): void {
    this.keyManager = new ListKeyManager(this.renderedItems)
      .withVerticalOrientation()
      .withHomeAndEnd()
      .withWrap()
      .withTypeAhead();

    this.keyManager.change
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(itemIdx => {
        this.renderedItems.forEach(item => {
          item.focused = false;
        })

        this.keyManager.activeItem!.focused = true;

        // the item might be out-of-view so make sure
        // we scroll enough to have it inside the view
        const scrollable = this.scrollableList?.nativeElement;
        if (!!scrollable) {
          const active = this.keyManager.activeItem!.elementRef.nativeElement;
          const activeHeight = active.getBoundingClientRect().height;
          const bottom = scrollable.scrollTop + scrollable.getBoundingClientRect().height;
          const top = scrollable.scrollTop;

          let scrollTo = -1;
          if (active.offsetTop >= bottom) {
            scrollTo = top + active.offsetTop - bottom + activeHeight;
          } else if (active.offsetTop < top) {
            scrollTo = active.offsetTop;
          }

          if (scrollTo > -1) {
            scrollable.scrollTo({
              behavior: 'smooth',
              top: scrollTo,
            })
          }
        }

        this.cdr.markForCheck();
      })


    combineLatest([
      this.userProvidedItems!.changes
        .pipe(startWith(undefined)),
      this.search$
    ])
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(
        ([_, search]) => {
          this.updateItems();

          search = (search || '').toLocaleLowerCase()
          let items: SelectOption[] = [];
          if (search === '') {
            items = this.allItems!;
          } else {
            items = this.allItems!.filter(item => {
              // we always count selected items as a "match" in search mode.
              // this is to ensure the user always see all selected items.
              if (item.selected) {
                return true;
              }

              if (!!item.value && typeof item.value === 'string') {
                if (item.value.toLocaleLowerCase().includes(search)) {
                  return true;
                }
              }

              if (!!item.label) {
                if (item.label.toLocaleLowerCase().includes(search)) {
                  return true
                }
              }
              return false;
            })
          }

          this.items = items.slice(0, this._maxItemLimit);
          this.keyManager.setActiveItem(0);

          this.cdr.detectChanges();
        }
      );
  }

  ngOnDestroy(): void {
    this.search$.complete();
  }

  @HostListener('blur')
  onBlur(): void {
    this.onTouch();
  }

  /** @private - called when the internal dropdown opens */
  onDropdownOpen() {
    // emit the open event on this component as well
    this.opened.next();

    // reset the search. We do that when opened instead of closed
    // to avoid flickering when the component height increases
    // during the "close" animation
    this.onSearch('');
  }

  /** @private - called when the internal dropdown closes */
  onDropdownClose() {
    this.closed.next();
  }

  onSearch(text: string) {
    this.searchText = text;
    this.search$.next(text);
  }

  selectItem(item: SelectOption) {
    if (item.disabled) {
      return;
    }

    const isSelected = this.currentItems.findIndex(selected => item.value === selected.value);
    if (isSelected === -1) {
      item.selected = true;

      if (this.mode === 'single') {
        this.currentItems.forEach(i => i.selected = false);
        this.currentItems = [item];
        this.value = item.value;
      } else {
        this.currentItems.push(item);
        // TODO(ppacher): somehow typescript does not correctly pick up
        // the type of this.value here although it can be infered from the
        // mode === 'single' check above.
        this.value = [
          ...(this.value || []) as any,
          item.value,
        ] as any
      }
    } else if (this.mode !== 'single') { // "unselecting" a value is not allowed in single mode
      this.currentItems.splice(isSelected, 1)
      item.selected = false;
      // same note about typescript as above.
      this.value = (this.value as T[]).filter(val => val !== item.value) as any;
    }

    // only close the drop down in single mode. In multi-mode
    // we keep it open as the user might want to select an additional
    // item as well.
    if (this.mode === 'single') {
      this.dropdown?.close();
    }
    this.onChange(this.value!);
  }

  private updateItems() {
    let values: T[] = [];
    if (this.mode === 'single') {
      values = [this.value as T];
    } else {
      values = (this.value as T[]) || [];
    }

    this.currentItems = [];
    this.allItems = [];

    // mark all user-selected items as "deselected" first
    this.userProvidedItems?.forEach(item => {
      item.selected = false;
      this.allItems.push(item);
    });

    for (let i = 0; i < values.length; i++) {
      const val = values[i];
      let option: SelectOption | undefined = this.userProvidedItems?.find(item => item.value === val);
      if (!option) {
        if (!this._dynamicValues) {
          continue
        }

        option = {
          selected: true,
          value: val,
          label: `${val}`,
        }
        this.allItems.push(option);
      } else {
        option.selected = true
      }

      this.currentItems.push(option);
    }

    if (this.sortValues) {
      this.allItems.sort((a, b) => {
        if (b.selected && !a.selected) {
          return 1;
        }

        if (a.selected && !b.selected) {
          return -1;
        }

        return this.sortBy(a, b)
      })
    }
  }

  writeValue(value: SelectValue<T, this>): void {
    this.value = value;

    this.updateItems();

    this.cdr.markForCheck();
  }

  onChange = (value: SelectValue<T, this>): void => { }
  registerOnChange(fn: (value: SelectValue<T, this>) => void): void {
    this.onChange = fn;
  }

  onTouch = (): void => { }
  registerOnTouched(fn: () => void): void {
    this.onTouch = fn;
  }
}
