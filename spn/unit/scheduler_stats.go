package unit

// Stats are somewhat racy, as one value of sum or count might already be
// updated with the latest slot data, while the other has been not.
// This is not so much of a problem, as slots are really short and the impact
// is very low.

// cycleStats calculates the new values and cycles the current values.
func (s *Scheduler) cycleStats() {
	// Get and reset max pace.
	s.stats.current.maxPace.Store(s.stats.progress.maxPace.Load())
	s.stats.progress.maxPace.Store(0)

	// Get and reset max leveled pace.
	s.stats.current.maxLeveledPace.Store(s.stats.progress.maxLeveledPace.Load())
	s.stats.progress.maxLeveledPace.Store(0)

	// Get and reset avg slot pace.
	avgPaceCnt := s.stats.progress.avgPaceCnt.Load()
	if avgPaceCnt > 0 {
		s.stats.current.avgPace.Store(s.stats.progress.avgPaceSum.Load() / avgPaceCnt)
	} else {
		s.stats.current.avgPace.Store(0)
	}
	s.stats.progress.avgPaceCnt.Store(0)
	s.stats.progress.avgPaceSum.Store(0)

	// Get and reset avg unit life.
	avgUnitLifeCnt := s.stats.progress.avgUnitLifeCnt.Load()
	if avgUnitLifeCnt > 0 {
		s.stats.current.avgUnitLife.Store(s.stats.progress.avgUnitLifeSum.Load() / avgUnitLifeCnt)
	} else {
		s.stats.current.avgUnitLife.Store(0)
	}
	s.stats.progress.avgUnitLifeCnt.Store(0)
	s.stats.progress.avgUnitLifeSum.Store(0)

	// Get and reset avg work slot duration.
	avgWorkSlotCnt := s.stats.progress.avgWorkSlotCnt.Load()
	if avgWorkSlotCnt > 0 {
		s.stats.current.avgWorkSlot.Store(s.stats.progress.avgWorkSlotSum.Load() / avgWorkSlotCnt)
	} else {
		s.stats.current.avgWorkSlot.Store(0)
	}
	s.stats.progress.avgWorkSlotCnt.Store(0)
	s.stats.progress.avgWorkSlotSum.Store(0)

	// Get and reset avg catch up slot duration.
	avgCatchUpSlotCnt := s.stats.progress.avgCatchUpSlotCnt.Load()
	if avgCatchUpSlotCnt > 0 {
		s.stats.current.avgCatchUpSlot.Store(s.stats.progress.avgCatchUpSlotSum.Load() / avgCatchUpSlotCnt)
	} else {
		s.stats.current.avgCatchUpSlot.Store(0)
	}
	s.stats.progress.avgCatchUpSlotCnt.Store(0)
	s.stats.progress.avgCatchUpSlotSum.Store(0)
}

// GetMaxSlotPace returns the current maximum slot pace.
func (s *Scheduler) GetMaxSlotPace() int64 {
	return s.stats.current.maxPace.Load()
}

// GetMaxLeveledSlotPace returns the current maximum leveled slot pace.
func (s *Scheduler) GetMaxLeveledSlotPace() int64 {
	return s.stats.current.maxLeveledPace.Load()
}

// GetAvgSlotPace returns the current average slot pace.
func (s *Scheduler) GetAvgSlotPace() int64 {
	return s.stats.current.avgPace.Load()
}

// GetAvgUnitLife returns the current average unit lifetime until it is finished.
func (s *Scheduler) GetAvgUnitLife() int64 {
	return s.stats.current.avgUnitLife.Load()
}

// GetAvgWorkSlotDuration returns the current average work slot duration.
func (s *Scheduler) GetAvgWorkSlotDuration() int64 {
	return s.stats.current.avgWorkSlot.Load()
}

// GetAvgCatchUpSlotDuration returns the current average catch up slot duration.
func (s *Scheduler) GetAvgCatchUpSlotDuration() int64 {
	return s.stats.current.avgCatchUpSlot.Load()
}
