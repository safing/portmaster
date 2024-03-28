import { ChangeDetectionStrategy, ChangeDetectorRef, Component, ContentChild, Directive, EventEmitter, Input, OnChanges, OnDestroy, Output, SimpleChanges, TemplateRef } from "@angular/core";
import { Observable, Subscription } from "rxjs";

export interface Pagination<T> {
  /**
   * Total should return the total number of pages
   */
  total: number;

  /**
   * pageNumber$ should emit the currently displayed page
   */
  pageNumber$: Observable<number>;

  /**
   * pageItems$ should emit all items of the current page
   */
  pageItems$: Observable<T[]>;

  /**
   * nextPage should progress to the next page. If there are no more
   * pages than nextPage() should be a no-op.
   */
  nextPage(): void;

  /**
   * prevPage should move back the the previous page. If there is no
   * previous page, prevPage should be a no-op.
   */
  prevPage(): void;

  /**
   * openPage opens the page @pageNumber. If pageNumber is greater than
   * the total amount of pages it is clipped to the lastPage. If it is
   * less than 1, it is clipped to 1.
   */
  openPage(pageNumber: number): void
}



@Directive({
  selector: '[sfngPageContent]'
})
export class SfngPaginationContentDirective<T = any> {
  constructor(public readonly templateRef: TemplateRef<T>) { }
}

export interface PageChangeEvent {
  totalPages: number;
  currentPage: number;
}

@Component({
  selector: 'sfng-pagination',
  templateUrl: './pagination.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngPaginationWrapperComponent<T = any> implements OnChanges, OnDestroy {
  private _sub: Subscription = Subscription.EMPTY;

  @Input()
  source: Pagination<T> | null = null;

  @Output()
  pageChange = new EventEmitter<PageChangeEvent>();

  @ContentChild(SfngPaginationContentDirective)
  content: SfngPaginationContentDirective | null = null;

  currentPageIdx: number = 0;
  pageNumbers: number[] = [];

  ngOnChanges(changes: SimpleChanges) {
    if ('source' in changes) {
      this.subscribeToSource(changes.source.currentValue);
    }
  }

  ngOnDestroy() {
    this._sub.unsubscribe();
  }

  private subscribeToSource(source: Pagination<T>) {
    // Unsubscribe from the previous pagination, if any
    this._sub.unsubscribe();

    this._sub = new Subscription();

    this._sub.add(
      source.pageNumber$
        .subscribe(current => {
          this.currentPageIdx = current;
          this.pageNumbers = generatePageNumbers(current - 1, source.total);
          this.cdr.markForCheck();

          this.pageChange.next({
            totalPages: source.total,
            currentPage: current,
          })
        })
    )
  }

  constructor(private cdr: ChangeDetectorRef) { }
}

/**
 * Generates an array of page numbers that should be displayed in paginations.
 *
 * @param current The current page number
 * @param countPages The total number of pages
 * @returns An array of page numbers to display
 */
export function generatePageNumbers(current: number, countPages: number): number[] {
  let delta = 2;
  let leftRange = current - delta;
  let rightRange = current + delta + 1;

  return Array.from({ length: countPages }, (v, k) => k + 1)
    .filter(i => i === 1 || i === countPages || (i >= leftRange && i < rightRange));
}

export function clipPage(pageNumber: number, total: number): number {
  if (pageNumber < 1) {
    return 1;
  }
  if (pageNumber > total) {
    return total;
  }
  return pageNumber;
}
