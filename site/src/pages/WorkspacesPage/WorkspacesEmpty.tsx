import ArrowForwardOutlined from "@mui/icons-material/ArrowForwardOutlined";
import Button from "@mui/material/Button";
import { Template } from "api/typesGenerated";
import { Avatar } from "components/Avatar/Avatar";
import { TableEmpty } from "components/TableEmpty/TableEmpty";
import { Link } from "react-router-dom";

export const WorkspacesEmpty = (props: {
  isUsingFilter: boolean;
  templates?: Template[];
}) => {
  const { isUsingFilter, templates } = props;
  const totalFeaturedTemplates = 6;
  const featuredTemplates = templates?.slice(0, totalFeaturedTemplates);

  if (isUsingFilter) {
    return <TableEmpty message="No results matched your search" />;
  }

  return (
    <TableEmpty
      message="Create a workspace"
      description="A workspace is your personal, customizable development environment in the cloud. Select one template below to start."
      cta={
        <div>
          <div
            css={(theme) => ({
              display: "flex",
              flexWrap: "wrap",
              gap: theme.spacing(2),
              marginBottom: theme.spacing(3),
              justifyContent: "center",
              maxWidth: "800px",
            })}
          >
            {featuredTemplates?.map((t) => (
              <Link
                to={`/templates/${t.name}/workspace`}
                key={t.id}
                css={(theme) => ({
                  width: "320px",
                  padding: theme.spacing(2),
                  borderRadius: 6,
                  border: `1px solid ${theme.palette.divider}`,
                  textAlign: "left",
                  display: "flex",
                  gap: theme.spacing(2),
                  textDecoration: "none",
                  color: "inherit",

                  "&:hover": {
                    backgroundColor: theme.palette.background.paperLight,
                  },
                })}
              >
                <div css={{ flexShrink: 0, paddingTop: 4 }}>
                  <Avatar
                    variant={t.icon ? "square" : undefined}
                    fitImage={Boolean(t.icon)}
                    src={t.icon}
                    size="sm"
                  >
                    {t.name}
                  </Avatar>
                </div>
                <div>
                  <h4 css={{ fontSize: 14, fontWeight: 600, margin: 0 }}>
                    {t.display_name}
                  </h4>
                  <span
                    css={(theme) => ({
                      fontSize: 13,
                      color: theme.palette.text.secondary,
                      lineHeight: "0.5",
                    })}
                  >
                    {t.description}
                  </span>
                </div>
              </Link>
            ))}
          </div>
          {templates && templates.length > totalFeaturedTemplates && (
            <Button
              component={Link}
              to="/templates"
              variant="contained"
              startIcon={<ArrowForwardOutlined />}
            >
              See all templates
            </Button>
          )}
        </div>
      }
    />
  );
};