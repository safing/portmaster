import { ListKeyManager } from "@angular/cdk/a11y";
import { coerceBooleanProperty } from "@angular/cdk/coercion";
import { CdkOverlayOrigin } from "@angular/cdk/overlay";
import { AfterViewInit, ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, Directive, ElementRef, EventEmitter, forwardRef, HostBinding, HostListener, inject, Input, OnDestroy, OnInit, Output, QueryList, TrackByFunction, ViewChild, ViewChildren } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { ControlValueAccessor, NG_VALUE_ACCESSOR } from "@angular/forms";
import { Condition, ExpertiseLevel, Netquery, NetqueryConnection } from "@safing/portmaster-api";
import { SfngDropdownComponent } from "@safing/ui";
import { combineLatest, Observable, of, Subject } from "rxjs";
import { catchError, debounceTime, map, switchMap } from "rxjs/operators";
import { fadeInAnimation, fadeInListAnimation } from "../../animations";
import { ExpertiseService } from "../../expertise";
import { objKeys } from "../../utils";
import { NetqueryHelper } from "../connection-helper.service";
import { Parser, ParseResult } from "../textql";

export type SfngSearchbarFields = {
  [key in keyof Partial<NetqueryConnection>]: any[];
} & {
  groupBy?: string[];
  orderBy?: string[];
  from?: string[];
  to?: string[];
}

export type SfngSearchbarSuggestionValue<K extends keyof NetqueryConnection> = {
  value: NetqueryConnection[K];
  display: string;
  count: number;
}

export type SfngSearchbarSuggestion<K extends keyof NetqueryConnection> = {
  start?: number;
  field: K | '_textsearch';
  values: SfngSearchbarSuggestionValue<K>[];
}

@Directive({
  selector: '[sfngNetquerySuggestion]',
  exportAs: 'sfngNetquerySuggestion'
})
export class SfngNetquerySuggestionDirective<K extends keyof NetqueryConnection> {
  constructor() { }

  @Input()
  sfngSuggestion?: SfngSearchbarSuggestion<K>;

  @Input()
  sfngNetquerySuggestion?: SfngSearchbarSuggestionValue<K> | string;

  set active(v: any) {
    this._active = coerceBooleanProperty(v);
  }
  get active() {
    return this._active;
  }
  private _active: boolean = false;

  getLabel(): string {
    if (typeof this.sfngNetquerySuggestion === 'string') {
      return this.sfngNetquerySuggestion;
    }
    return '' + this.sfngNetquerySuggestion?.value;
  }
}

@Component({
  selector: 'sfng-netquery-searchbar',
  templateUrl: './searchbar.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    fadeInAnimation,
    fadeInListAnimation
  ],
  providers: [
    {
      provide: NG_VALUE_ACCESSOR,
      useExisting: forwardRef(() => SfngNetquerySearchbarComponent),
      multi: true,
    }
  ]
})
export class SfngNetquerySearchbarComponent implements ControlValueAccessor, OnInit, OnDestroy, AfterViewInit {
  private loadSuggestions$ = new Subject<void>();
  private triggerDropdownClose$ = new Subject<boolean>();
  private keyManager!: ListKeyManager<SfngNetquerySuggestionDirective<any>>;
  private destroyRef = inject(DestroyRef);

  /** Whether or not we are currently loading suggestions */
  loading = false;

  @ViewChild(CdkOverlayOrigin, { static: true })
  searchBoxOverlayOrigin!: CdkOverlayOrigin;

  @ViewChild(SfngDropdownComponent)
  suggestionDropDown?: SfngDropdownComponent;

  @ViewChild('searchBar', { static: true, read: ElementRef })
  searchBar!: ElementRef;

  @ViewChildren(SfngNetquerySuggestionDirective)
  suggestionValues!: QueryList<SfngNetquerySuggestionDirective<any>>;

  @Output()
  fieldsParsed = new EventEmitter<SfngSearchbarFields>();

  @Input()
  labels: { [key: string]: string } = {}

  /** Controls whether or not suggestions are shown as a drop-down or inline */
  @Input()
  mode: 'inline' | 'default' = 'default';

  suggestions: SfngSearchbarSuggestion<any>[] = [];

  textSearch = '';

  @HostListener('focus')
  onFocus() {
    // move focus forward to the input element
    this.searchBar.nativeElement.focus();
  }

  @Input()
  @HostBinding('tabindex')
  tabindex = 0;

  writeValue(val: string): void {
    if (typeof val === 'string') {
      const result = Parser.parse(val);
      this.textSearch = result.textQuery;
    } else {
      this.textSearch = '';
    }
    this.cdr.markForCheck();
  }

  _onChange: (val: string) => void = () => { }
  registerOnChange(fn: any): void {
    this._onChange = fn;
  }

  _onTouched: () => void = () => { }
  registerOnTouched(fn: any): void {
    this._onTouched = fn
  }

  ngAfterViewInit(): void {
    this.keyManager = new ListKeyManager(this.suggestionValues)
      .withVerticalOrientation()
      .withTypeAhead()
      .withHomeAndEnd()
      .withWrap();

    this.keyManager.change
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(idx => {
        if (!this.suggestionValues.length) {
          return
        }

        this.suggestionValues.forEach(val => val.active = false);
        this.suggestionValues.get(idx)!.active = true;
        this.cdr.markForCheck();
      });
  }

  ngOnInit(): void {
    this.loadSuggestions$
      .pipe(
        debounceTime(500),
        switchMap(() => {
          let fields: (keyof NetqueryConnection)[] = [
            'profile',
            'domain',
            'as_owner',
            'remote_ip',
          ];
          let limit = 3;

          const parser = new Parser(this.textSearch);
          const parseResult = parser.process();

          const queries: Observable<SfngSearchbarSuggestion<any>>[] = [];
          const queryKeys: (keyof Partial<NetqueryConnection>)[] = [];

          // FIXME(ppacher): confirm .type is an actually allowed field
          if (!!parser.lastUnterminatedCondition) {
            fields = [parser.lastUnterminatedCondition.type as keyof NetqueryConnection];
            limit = 0;
          }

          fields.forEach(field => {
            let queryField = field;

            // if we are searching the profiles we use the profile name
            // rather than the profile_id for searching.
            if (field === 'profile') {
              queryField = 'profile_name';
            }

            const query: Condition = {
              [queryField]: {
                $like: `%${!!parser.lastUnterminatedCondition ? parser.lastUnterminatedCondition.value : parseResult.textQuery}%`
              },
            }

            // hide internal connections if the user is not a developer
            if (this.expertiseService.currentLevel !== ExpertiseLevel.Developer) {
              query.internal = {
                $eq: false
              }
            }

            const obs = this.netquery.query({
              select: [
                field,
                {
                  $count: {
                    field: "*",
                    as: "count"
                  },
                }
              ],
              query: query,
              groupBy: [
                field,
              ],
              page: 0,
              pageSize: limit,
              orderBy: [{ field: "count", desc: true }]
            }, 'netquery-searchbar-get-counts')
              .pipe(
                this.helper.encodeToPossibleValues(field),
                map(results => {
                  let val: SfngSearchbarSuggestion<typeof field> = {
                    field: field,
                    values: [],
                    start: parser.lastUnterminatedCondition ? parser.lastUnterminatedCondition.start : undefined,
                  }

                  results.forEach(res => {
                    val.values.push({
                      value: res.Value,
                      display: res.Name,
                      count: res.count,
                    })
                  })

                  return val;
                }),
                catchError(err => {
                  console.error(err);

                  return of({
                    field: field,
                    values: [],
                  })
                })
              )

            queries.push(obs)
            queryKeys.push(field)
          })

          return combineLatest(queries)
        }),
      )
      .subscribe(result => {
        this.loading = false;

        this.suggestions = result
          .filter((sug: SfngSearchbarSuggestion<any>) => sug.values?.length > 0)

        this.keyManager.setActiveItem(0);

        this.cdr.markForCheck();
      })

    this.triggerDropdownClose$
      .pipe(debounceTime(100))
      .subscribe(shouldClose => {
        if (shouldClose) {
          this.suggestionDropDown?.close();
        }
      })

    if (this.mode === 'inline') {
      this.loadSuggestions();
    }
  }

  ngOnDestroy(): void {
    this.loadSuggestions$.complete();
    this.triggerDropdownClose$.complete();
  }

  cancelDropdownClose() {
    this.triggerDropdownClose$.next(false);
  }

  onSearchModelChange(value: string) {
    if (value.length >= 3 || this.mode === 'inline') {
      this.loadSuggestions();
    } else if (this.suggestionDropDown?.isOpen) {
      // close the suggestion dropdown if the search input contains less than
      // 3 characters and we're currently showing the dropdown
      this.suggestionDropDown?.close();
    }
  }

  /** @private Callback for keyboard events on the search-input */
  onSearchKeyDown(event: KeyboardEvent) {
    if (event.key === ' ' && event.ctrlKey) {
      this.loadSuggestions();
      event.preventDefault();
      event.stopPropagation()
      return;
    }

    if (event.key === 'Enter') {

      const selectedSuggestion = this.suggestionValues.toArray().findIndex(val => val.active);
      if (selectedSuggestion > 0) { // we must skip 0 here as well as that's the dummy element
        const sug = this.suggestionValues.get(selectedSuggestion);
        this.applySuggestion(sug?.sfngSuggestion?.field, sug?.sfngNetquerySuggestion, event, sug?.sfngSuggestion?.start)

        return;
      }

      this.suggestionDropDown?.close();
      this.parseAndEmit();
      this.cdr.markForCheck();

      return;
    }

    this.keyManager.onKeydown(event);
  }

  onFocusLost(event: FocusEvent) {
    this._onTouched();
  }

  private parseAndEmit() {
    const result = Parser.parse(this.textSearch);
    this.textSearch = result.textQuery;

    const keys = objKeys(result.conditions)
    const meta = {
      groupBy: result.groupBy || undefined,
      orderBy: result.orderBy || undefined,
    }
    if (keys.length > 0 || meta.groupBy?.length || meta.orderBy?.length) {
      let updatedConditions: ParseResult['conditions'] = {};
      keys.forEach(key => {
        updatedConditions[key] = this.helper.decodePrettyValues(key as keyof NetqueryConnection, result.conditions[key])
      })
      this.fieldsParsed.next({ ...updatedConditions, ...meta });
    }

    this._onChange(this.textSearch);
  }

  applySuggestion(field: keyof NetqueryConnection | '_textsearch', val: any, event: { shiftKey: boolean }, start?: number) {
    // this is a full-text search so just emit the value, close the dropdown and we're done
    if (field === '_textsearch') {
      this._onChange(this.textSearch);
      this.suggestionDropDown?.close();

      return
    }

    if (start !== undefined) {
      this.textSearch = this.textSearch.slice(0, start)
    } else if (!event.shiftKey) {
      this.textSearch = '';
    } else {
      // the user pressed shift-key and used free-text search so we remove
      // the remaining part
      const parseRes = Parser.parse(this.textSearch);
      let query = "";
      objKeys(parseRes.conditions).forEach(field => {
        parseRes.conditions[field]?.forEach(value => {
          query += `${field}:${JSON.stringify(value)} `
        })
      })
      this.textSearch = query;
    }

    if (event.shiftKey) {
      const textqlVal = `${field}:${JSON.stringify(val)}`
      if (!this.textSearch.includes(textqlVal)) {
        if (this.textSearch !== '') {
          this.textSearch += " "
        }
        this.textSearch += textqlVal + " "
        this.triggerDropdownClose$.next(false)
        // load new suggestions based on the new input
        this.loadSuggestions();
      }

      return;
    }

    // directly emit the new value and reset the text search
    this.fieldsParsed.next({
      [field]: [val]
    })

    // parse and emit the current search field but without the suggestion value
    this.parseAndEmit();

    this.suggestionDropDown?.close();

    this.cdr.markForCheck();
  }

  resetKeyboardSelection() {
    this.keyManager.setActiveItem(0);
  }

  loadSuggestions() {
    this.loading = true;
    this.loadSuggestions$.next();
    this.suggestionDropDown?.show(this.searchBoxOverlayOrigin)
  }

  trackSuggestion: TrackByFunction<SfngSearchbarSuggestion<any>> = (_: number, val: SfngSearchbarSuggestion<any>) => val.field;

  constructor(
    private cdr: ChangeDetectorRef,
    private expertiseService: ExpertiseService,
    private netquery: Netquery,
    private helper: NetqueryHelper,
  ) { }
}
