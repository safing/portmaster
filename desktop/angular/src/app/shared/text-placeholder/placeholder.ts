import { AfterContentChecked, ChangeDetectionStrategy, ChangeDetectorRef, Component, ElementRef, Input } from '@angular/core';

@Component({
  selector: 'app-text-placeholder',
  template: `
    <span class="text-placeholder" *ngIf="loading">
      <div class="background" [style.width]="width" ></div>
    </span>
    <ng-content *ngIf="mode === 'auto' || !loading"></ng-content>
  `,
  styleUrls: ['./placeholder.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PlaceholderComponent implements AfterContentChecked {
  @Input()
  set width(v: string | number) {
    if (typeof v === 'number') {
      this._width = `${v}px`;
      return
    }

    switch (v) {
      case 'small':
        this._width = '5rem';
        break;
      case 'medium':
        this._width = '10rem';
        break;
      case 'large':
        this._width = '15rem';
        break
      default:
        this._width = v;
    }
  }
  get width() { return this._width; }
  private _width: string = '10rem';

  @Input()
  mode: 'auto' | 'input' = 'auto';

  @Input()
  loading = true;

  constructor(
    private elementRef: ElementRef,
    private changeDetector: ChangeDetectorRef,
  ) { }

  ngAfterContentChecked() {
    if (this.mode === 'input') {
      return;
    }

    const show = this.elementRef.nativeElement.innerText === '';
    if (this.loading != show) {
      this.loading = show;
      this.changeDetector.detectChanges();
    }
  }
}
