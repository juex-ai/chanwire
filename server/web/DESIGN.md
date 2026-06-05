# Chanwire Web Console — Design Reference

Chanwire's web console follows the Refero Lpalo style: a playful, warm, blush canvas with bold rounded surfaces, heavy headings, crisp black outlines, and cheerful accent blocks.

## Tokens

| Name | Value | Role |
| --- | --- | --- |
| Canvas Pink | `#f6e0db` | Full-page background |
| Surface White | `#ffffff` | Cards, bubbles, panels |
| Charcoal Text | `#000000` | Text, borders, arrows |
| Pumpkin Accent | `#ef724f` | Primary actions and hot states |
| Lemon Highlight | `#e7db4c` | Small decorative highlights |
| Bubblegum Pink | `#981082` | Secondary accents |
| Spring Green | `#6ed311` | Online and success accents |
| Seafoam Accent | `#ace2df` | Node/avatar fills |
| Lavender Glow | `#e69dff` | Node/avatar fills |
| Sky Blue | `#84bfff` | Node/avatar fills |
| Deep Blue | `#5196ff` | Small precise accents |

## Typography

- Display: `Alfa Slab One`, fallback `Bebas Neue`, for prominent headers only.
- Body/UI: `Manrope`, fallback `Inter`/system sans, weights 400/500/700/800.
- Keep message content legible at 15–16px; reserve heavier weights for labels and agent names.

## Shape and Layout

- Use generous rounding: 47px for buttons, 40px for large playful panels, 10px for compact cards.
- Prefer solid fills and black outlines over shadows.
- Keep comfortable gaps around canvas nodes and message cards.
- Agent avatars should feel illustrated: black outline, simple face marks, colorful solid-fill backgrounds.

## Components

- **Canvas panel:** blush page around a white or lightly tinted rounded area with black outline.
- **Agent avatar:** circular/rounded illustrated badge, deterministic color from agent name, black outline, monogram and small face details.
- **Composer bubble:** white speech card near selected agent, black outline, rounded corners, textarea, pill action button.
- **Message list:** right-side white panel; each message has a small bold route label (`from -> to`) above readable body text.
