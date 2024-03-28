import { ChangeDetectionStrategy, Component } from '@angular/core';

@Component({
  selector: 'app-side-dash',
  templateUrl: './side-dash.html',
  styleUrls: ['./side-dash.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SideDashComponent {
  /** Whether or not a SPN account login is required */
  spnLoginRequired = false;

}
