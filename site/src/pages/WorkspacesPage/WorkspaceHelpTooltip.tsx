import {
	HelpTooltip,
	HelpTooltipContent,
	HelpTooltipLink,
	HelpTooltipLinksGroup,
	HelpTooltipText,
	HelpTooltipTitle,
	HelpTooltipTrigger,
} from "components/HelpTooltip/HelpTooltip";
import type { FC } from "react";
import { docs } from "utils/docs";

const Language = {
	workspaceTooltipTitle: "What is a workspace?",
	workspaceTooltipText:
		"A Wirtual workspace is a virtual desktop environment designed specifically for AI agents. It offers a cloud-based infrastructure that provides the necessary tools and resources for AI agents to operate in an augmented and effective manner.",
	workspaceTooltipLink1: "Create Workspaces",
	workspaceTooltipLink2: "Connect with SSH",
	workspaceTooltipLink3: "Editors and IDEs",
};

export const WorkspaceHelpTooltip: FC = () => {
	return (
		<HelpTooltip>
			<HelpTooltipTrigger />
			<HelpTooltipContent>
				<HelpTooltipTitle>{Language.workspaceTooltipTitle}</HelpTooltipTitle>
				<HelpTooltipText>{Language.workspaceTooltipText}</HelpTooltipText>
				<HelpTooltipLinksGroup>
					<HelpTooltipLink href={docs("/user-guides")}>
						{Language.workspaceTooltipLink1}
					</HelpTooltipLink>
					<HelpTooltipLink href={docs("/user-guides/workspace-access")}>
						{Language.workspaceTooltipLink2}
					</HelpTooltipLink>
				</HelpTooltipLinksGroup>
			</HelpTooltipContent>
		</HelpTooltip>
	);
};
