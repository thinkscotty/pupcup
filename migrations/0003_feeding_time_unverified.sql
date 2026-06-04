-- Feedings recorded on the device before its system clock synchronized (NTP)
-- carry an unverified timestamp. This build has no RTC, so a cold boot while
-- offline leaves the clock wrong until NTP catches up; the web app flags such
-- feedings so a human can confirm or correct the time. 0 = normal/verified,
-- 1 = time unverified. A human edit of the feeding clears it back to 0.
ALTER TABLE feedings ADD COLUMN time_unverified INTEGER NOT NULL DEFAULT 0;
