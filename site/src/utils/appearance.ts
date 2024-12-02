export const getApplicationName = (): string => {
	return "Wirtual";
};

export const getLogoURL = (): string => {
	return "site/static/icon/wirtual.png";
};

/**
 * Exposes an easy way to determine if a given URL is for an emoji hosted on
 * the Coder deployment.
 *
 * Helps when you need to style emojis differently (i.e., not adding rounding to
 * the container so that the emoji doesn't get cut off).
 */
export function isEmojiUrl(url: string | undefined): boolean {
	if (!url) {
		return false;
	}

	return url.startsWith("/emojis/");
}
