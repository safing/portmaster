import { ChangeDetectionStrategy, Component, Input, OnChanges, SimpleChanges } from '@angular/core';
import { ScopeTranslation } from '@safing/portmaster-api';
import { parseDomain } from '../../utils';

@Component({
  selector: 'sfng-netquery-scope-label',
  templateUrl: 'scope-label.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngNetqueryScopeLabelComponent implements OnChanges {
  readonly scopeTranslation = ScopeTranslation;

  @Input()
  scope?: string = ''

  @Input()
  set leftRightFix(v: any) {
    console.warn("deprecated @Input usage")
  }
  get leftRightFix() { return false }

  domain: string = '';
  subdomain: string = '';

  ngOnChanges(change: SimpleChanges) {
    if (!!change['scope']) {
      //this.label = change.label.currentValue;
      const result = parseDomain(change.scope.currentValue || '')

      this.domain = result?.domain || '';
      this.subdomain = result?.subdomain || '';
    }
  }
}
