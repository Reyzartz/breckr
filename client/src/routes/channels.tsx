import { createFileRoute } from "@tanstack/react-router";
import { ChannelsPage } from "../components/ChannelsPage.tsx";

export const Route = createFileRoute("/channels")({ component: ChannelsPage });
