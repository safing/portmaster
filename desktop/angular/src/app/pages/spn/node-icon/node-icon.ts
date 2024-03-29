import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ChangeDetectionStrategy, Component, Input } from "@angular/core";

@Component({
  // eslint-disable-next-line @angular-eslint/component-selector
  selector: 'spn-node-icon',
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './node-icon.html',
  styleUrls: ['./node-icon.scss'],
})
export class SpnNodeIconComponent {
  @Input()
  set bySafing(v: any) {
    this._bySafing = coerceBooleanProperty(v);
  }
  get bySafing() { return this._bySafing }
  private _bySafing = false;

  @Input()
  set isActive(v: any) {
    this._isActive = coerceBooleanProperty(v);
  }
  get isActive() { return this._isActive }
  private _isActive = false;

  @Input()
  set isExit(v: any) {
    this._isExit = coerceBooleanProperty(v);
  }
  get isExit() { return this._isExit; }
  private _isExit = false;

  get nodeClass() {
    if (this._isExit) {
      return 'exit';
    }

    if (this.isActive) {
      return 'active'
    }

    return '';
  }
}
