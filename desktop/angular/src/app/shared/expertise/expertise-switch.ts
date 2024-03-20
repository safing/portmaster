import { Component, ElementRef } from '@angular/core';
import { ExpertiseLevel } from '@safing/portmaster-api';
import { ExpertiseService } from './expertise.service';

@Component({
  selector: 'app-expertise',
  templateUrl: './expertise-switch.html',
  styleUrls: ['./expertise-switch.scss']
})
export class ExpertiseComponent {
  /** @private provide the expertise-level enums to the template */
  readonly expertiseLevels = ExpertiseLevel;

  currentLevel = this.expertiseService.change;

  /**
   * @private
   * Getter to access the expertise level as saved in the database
   */
  get savedLevel() {
    return this.expertiseService.savedLevel;
  }

  constructor(
    private expertiseService: ExpertiseService,
    public host: ElementRef<any>,
  ) { }

  /**
   * @private
   * Configures a new expertise level
   *
   * @param lvl The new expertise level to use
   */
  selectLevel(lvl: ExpertiseLevel) {
    this.expertiseService.setLevel(lvl);
  }
}
