import { BehaviorSubject, Observable } from "rxjs";
import { debounceTime, map } from "rxjs/operators";
import { clipPage, Pagination } from "./pagination";

export class SnapshotPaginator<T> implements Pagination<T> {
  private _itemSnapshot: T[] = [];
  private _activePageItems = new BehaviorSubject<T[]>([]);
  private _totalPages = 1;
  private _updatePending = false;

  constructor(
    public items$: Observable<T[]>,
    public readonly pageSize: number,
  ) {
    items$
      .pipe(debounceTime(100))
      .subscribe(data => {
        this._itemSnapshot = data;
        this.openPage(this._currentPage.getValue());
      });

    this._currentPage
      .subscribe(page => {
        this._updatePending = false;
        const start = this.pageSize * (page - 1);
        const end = this.pageSize * page;
        this._totalPages = Math.ceil(this._itemSnapshot.length / this.pageSize) || 1;
        this._activePageItems.next(this._itemSnapshot.slice(start, end));
      })
  }

  private _currentPage = new BehaviorSubject<number>(0);

  get updatePending() {
    return this._updatePending;
  }
  get pageNumber$(): Observable<number> {
    return this._activePageItems.pipe(map(() => this._currentPage.getValue()));
  }
  get pageNumber(): number {
    return this._currentPage.getValue();
  }
  get total(): number {
    return this._totalPages
  }
  get pageItems$(): Observable<T[]> {
    return this._activePageItems.asObservable();
  }
  get pageItems(): T[] {
    return this._activePageItems.getValue();
  }
  get snapshot(): T[] { return this._itemSnapshot };

  reload(): void { this.openPage(this._currentPage.getValue()) }

  nextPage(): void { this.openPage(this._currentPage.getValue() + 1) }

  prevPage(): void { this.openPage(this._currentPage.getValue() - 1) }

  openPage(pageNumber: number): void {
    pageNumber = clipPage(pageNumber, this.total);
    this._currentPage.next(pageNumber);
  }
}
