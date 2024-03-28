import { ChangeDetectionStrategy, Component, Input, OnChanges } from '@angular/core';

@Component({
  selector: 'app-count-indicator',
  templateUrl: './count-indicator.html',
  styleUrls: ['./count-indicator.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CountIndicatorComponent implements OnChanges {
  @Input()
  count = 0;

  @Input()
  countAllowed: number = 0;

  allowedPercentage: number = 0;

  ngOnChanges() {
    const ratio = (this.countAllowed / this.count) || 0;
    this.allowedPercentage = Math.round(ratio * 100);
  }
}
