import { Button, Text } from "brake-ui";
import type { Channel } from "../types/index.ts";
import { CHANNEL_TYPE_LABEL } from "../constants/index.ts";

interface ChannelPickerProps {
  channels: Channel[];
  /** Ids currently selected. */
  value: string[];
  onChange: (channelIds: string[]) => void;
  error?: string;
  /** Opens the channel manager, for when there is nothing to pick yet. */
  onManageChannels: () => void;
}

/**
 * Pick any number of channels for a task.
 *
 * Toggle chips rather than a multi-select: the same pattern the weekday row in
 * ScheduleField already uses, and with a handful of channels the whole set is
 * visible at once instead of hidden behind a dropdown.
 */
export function ChannelPicker({
  channels,
  value,
  onChange,
  error,
  onManageChannels,
}: ChannelPickerProps) {
  const toggle = (id: string) => {
    onChange(
      value.includes(id)
        ? value.filter((selected) => selected !== id)
        : [...value, id]
    );
  };

  return (
    <div className="grid grid-cols-1 gap-1">
      <Text variant="caption" color="muted">
        Alert via
      </Text>

      {channels.length === 0 ? (
        // A task can be saved with no channels, so this is a nudge rather than a
        // block — it just makes the consequence explicit before it costs an
        // alert.
        <Text variant="caption" color="muted">
          No channels yet — this task will record its history but never alert.{" "}
          <button
            type="button"
            className="cursor-pointer underline underline-offset-2"
            onClick={onManageChannels}
          >
            Add a channel
          </button>
          .
        </Text>
      ) : (
        <div className="flex flex-wrap gap-1">
          {channels.map((channel) => {
            const selected = value.includes(channel.id);
            return (
              <Button
                key={channel.id}
                type="button"
                size="sm"
                variant="outlined"
                color={selected ? "primary" : "secondary"}
                aria-pressed={selected}
                onClick={() => {
                  toggle(channel.id);
                }}
              >
                {channel.name}
                <Text variant="caption" color="muted" as="span">
                  {" "}
                  · {CHANNEL_TYPE_LABEL[channel.type]}
                  {!channel.enabled && " · muted"}
                </Text>
              </Button>
            );
          })}
        </div>
      )}

      {error && (
        <Text variant="caption" color="error">
          {error}
        </Text>
      )}

      {channels.length > 0 && value.length === 0 && (
        <Text variant="caption" color="muted">
          Nothing selected — this task will record its history but never alert.
        </Text>
      )}
    </div>
  );
}
