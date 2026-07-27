import { Button, Input, Select, Text } from "brake-ui";
import type { ScheduleFrequency } from "@breckr/shared";
import {
  FREQUENCY_OPTIONS,
  MAX_INTERVAL,
  WEEKDAY_LABELS,
} from "../constants/index.ts";

/**
 * The schedule's slice of the form state.
 *
 * Flat and string-typed like the rest of `FormState`: the structured
 * `Schedule` the server wants is assembled once at the submit boundary, the
 * same way `toSpec` assembles the spec. Fields the current frequency does not
 * use keep their last value, so flipping through the frequencies to compare
 * them does not wipe what was already typed.
 */
export interface ScheduleFields {
  frequency: ScheduleFrequency;
  /** Minutes or hours between runs, depending on the frequency. */
  interval: string;
  /** Minute past the hour, for the hourly frequency. */
  minuteOfHour: string;
  /** "HH:MM", for the daily, weekly and monthly frequencies. */
  time: string;
  /** Cron's day numbering, Sunday first. */
  weekdays: number[];
  monthDay: string;
  customCron: string;
}

interface ScheduleFieldProps {
  value: ScheduleFields;
  onChange: (patch: Partial<ScheduleFields>) => void;
  /** The server's complaint about the schedule, shown on the active control. */
  error?: string;
}

/**
 * Build a schedule without knowing cron.
 *
 * A frequency picks the shape; the controls beside it fill in that shape's
 * blanks. Nothing here produces a cron expression — the server converts the
 * structured result, and converts it back for editing — so the two directions
 * cannot drift apart.
 */
export function ScheduleField({ value, onChange, error }: ScheduleFieldProps) {
  const { frequency } = value;

  const handleFrequency = (event: React.ChangeEvent<HTMLSelectElement>) => {
    const next = event.target.value as ScheduleFrequency;
    const patch: Partial<ScheduleFields> = { frequency: next };

    // One box serves both interval frequencies, but hours stop at 23 — leaving
    // "every 45" in it after a switch would hand the server a rejection.
    if (next === "minutes" || next === "hours") {
      const current = Number.parseInt(value.interval, 10);
      const clamped = Number.isFinite(current)
        ? Math.min(Math.max(current, 1), MAX_INTERVAL[next])
        : 1;
      if (String(clamped) !== value.interval) patch.interval = String(clamped);
    }

    onChange(patch);
  };

  const toggleWeekday = (day: number) => {
    const selected = value.weekdays.includes(day);
    // Clearing the last day would leave a weekly schedule that never fires, so
    // the only way out of it is to pick a different day first.
    if (selected && value.weekdays.length === 1) return;

    onChange({
      weekdays: selected
        ? value.weekdays.filter((current) => current !== day)
        : [...value.weekdays, day].sort((a, b) => a - b),
    });
  };

  const timeInput = (
    <Input
      label="At"
      type="time"
      value={value.time}
      onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
        onChange({ time: event.target.value });
      }}
      fullWidth
    />
  );

  return (
    <div className="grid gap-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Select
          label="Schedule"
          value={frequency}
          onChange={handleFrequency}
          info="Times are in the server's timezone."
          fullWidth
        >
          {FREQUENCY_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>

        {frequency === "minutes" && (
          <Input
            label="Minutes between runs"
            type="number"
            min={1}
            max={MAX_INTERVAL.minutes}
            value={value.interval}
            onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
              onChange({ interval: event.target.value });
            }}
            error={error}
            info="An interval that does not divide an hour restarts on the hour."
            fullWidth
          />
        )}

        {frequency === "hours" && (
          <>
            <Input
              label="Hours between runs"
              type="number"
              min={1}
              max={MAX_INTERVAL.hours}
              value={value.interval}
              onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
                onChange({ interval: event.target.value });
              }}
              error={error}
              fullWidth
            />
            <Input
              label="At minute past the hour"
              type="number"
              min={0}
              max={59}
              value={value.minuteOfHour}
              onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
                onChange({ minuteOfHour: event.target.value });
              }}
              fullWidth
            />
          </>
        )}

        {frequency === "day" && timeInput}

        {frequency === "week" && timeInput}

        {frequency === "month" && (
          <>
            <Input
              label="Day of the month"
              type="number"
              min={1}
              max={31}
              value={value.monthDay}
              onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
                onChange({ monthDay: event.target.value });
              }}
              error={error}
              info="Months without that day are skipped."
              fullWidth
            />
            {timeInput}
          </>
        )}

        {frequency === "custom" && (
          <Input
            label="Cron expression"
            value={value.customCron}
            onChange={(event: React.ChangeEvent<HTMLInputElement>) => {
              onChange({ customCron: event.target.value });
            }}
            error={error}
            placeholder="*/15 * * * *"
            info="Standard 5-field cron."
            fullWidth
          />
        )}
      </div>

      {frequency === "week" && (
        <div className="grid gap-1">
          <Text variant="caption" color="muted">
            On
          </Text>
          <div className="flex flex-wrap gap-1">
            {WEEKDAY_LABELS.map((label, day) => {
              const selected = value.weekdays.includes(day);
              return (
                <Button
                  key={label}
                  type="button"
                  size="sm"
                  variant="outlined"
                  color={selected ? "primary" : "default"}
                  aria-pressed={selected}
                  onClick={() => {
                    toggleWeekday(day);
                  }}
                >
                  {label}
                </Button>
              );
            })}
          </div>
          {error && (
            <Text variant="caption" color="error">
              {error}
            </Text>
          )}
        </div>
      )}
    </div>
  );
}
