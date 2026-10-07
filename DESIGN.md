# Design: mascot and visual identity

Reference for the visuals of `dot`. The master illustration (shell centered and small, thin halos on the dots) was validated on 2026-10-07; every deliverable is generated from it. The source images live outside the repository.

## Concept

The mascot is a **ladybug**. The pun is the name itself: a ladybug is a round body with dots, and `dot` is a dot.

It follows the pattern of the sibling projects:

| Project | Animal | Link to the project |
| --- | --- | --- |
| gekko | leopard gecko | name pun, `>` glowing on the tail, desert night with circuits |
| shellkit | hermit crab | the shell, wrench and screwdriver, beach sunset |
| dot | ladybug | the dots, one per profile, a guardian that hunts leaks |

- **Role**: a guardian that links and watches. It hunts leaks and secrets, holds the profiles and weaves the links.
- **Tone**: cheerful and a little sly.
- **Internal name**: Dotty. It is used only to talk about the character and must **not** appear anywhere (README, site, CLI, logo, copy).

## Validated character (master illustration)

The master is a single full-frame illustration (`master`, three-quarter view, square). Every other visual derives from it: attach it as reference, never a drifted sheet.

- **Posture**: upright **biped** on two simple, human-like legs (straight, slightly thick, tapered, a hint of knee and ankle) ending in rounded shoe-like feet. No thin insect legs, no row of ball-shaped toes.
- **Four arms**, slim, soft and rubbery, with small round-padded hands: the upper right hand raises the magnifying glass (golden rim, mint lens with a padlock reflection), the upper left hand raises a small ornate golden key; the lower pair is relaxed, one hand on the belly and one on the hip.
- **Head**: big, about 40 percent of the figure's height, the dominant element. Black head and pronotum with two cream-white corner patches (the signature of a ladybug), cream face, huge glossy amber-brown eyes (radial iris, black pupil, two white highlights, thick upper lid line, lighter ring), wide smile with a pink tongue. **No eyelashes, no blush.**
- **Antennae**: thin, segmented, ending in red round beads.
- **Body**: compact, cream belly with fine segment lines.
- **Shell**: on the **back**, **centered behind the torso**. Two elytra meet along a center line that belongs **in the middle of the back**: it runs down the body's axis, directly behind the belly, hidden by the torso (at most it peeks out right below the pronotum and below the belly). On each side of the torso only the outer curve of **one** elytron shows, a smooth rounded red surface with its own dots, **with no dividing line and no seam on the side**. The shell is **small in proportion to the body**, never a huge shield: it extends beyond the torso by only about 15 percent of the torso's width on each side, rises only slightly above the shoulders and ends around the hips; the head stays the dominant element. It is symmetric and centered behind the torso. The belly, chest, arms, legs and head stay in front of it. Never a disc stuck to one side of the body, never a shield carried in front. The center line, where it shows (back view, top view, logo), is slightly wobbly and hand-drawn. Smooth organic lacquered chitin, deep red at the edges to bright red on top, very subtle grain, two or three small soft highlights. Always clearly red, never brown.
- **7 black dots** (3 per elytron + 1 central), hand-cut, slightly different sizes and edges. **Every dot carries the same soft phosphor lime green halo**, like a bioluminescent aura, equal in style and intensity (never an outline, never a change of the dot's size). Since all the dots glow, no single dot has to be shown: in the three-quarter view the central dot may be hidden behind the torso. No dot on the forehead.
- **Accessories**: the magnifying glass (guard) and the key (secrets). No other tool.

### Symbolic details

| Element | Meaning |
| --- | --- |
| The 7 dots | One dot per profile. Each dot is a `.`, the `.` of `dot`. |
| The lime halo on every dot | Replaces gekko's orange `>`: the phosphor green of a terminal. Because all the dots glow, the focus is not on one spot of the back. In the site animation, a halo can light up per installed profile. |
| Magnifying glass and padlock | The guard: leaks and secrets. |
| Key | `dot secrets`. |

## Rendering

Semi-realistic cartoon sticker, the same finish as the Gekko and Shellkit illustrations:

- **Thick, bold, uniform dark outline** (`#14101E`) around the whole figure, the eyes and every object, as thick as the Gekko avatar in proportion to the frame, plus a thin soft warm luminous rim just outside it.
- Rich painted shading inside (soft gradients, warm bounce light), saturated glowing colors, realistic materials with a cartoon expression.
- Never: a 3D render or vinyl-toy look, plastic gloss, a flat vector look, a manga look, a soft painting without line work, an orange-peel or pitted texture.

## Palette

| Use | Color |
| --- | --- |
| Ladybug red | `#E5322D`, shadow `#B3201F` |
| Ink | `#14101E` |
| Twilight sky | gradient `#2A1B4D` to `#6B3FA0` |
| Foliage | `#3FAE6A` |
| Lime glow, fireflies | `#B6F34A`, mint `#5EF2C8` |
| Cream (face, belly) | warm cream |
| Gold (key, glass rim) | warm gold |

No orange anywhere. Phosphor green is the complement of red, so the glow stands out. The violet, green and red palette sets `dot` apart from gekko and shellkit, which are both orange.

## Hero scene

A garden at twilight seen from ground level: tall grass and large leaves, violet sky. On the leaf veins run glowing lime-green lines, the symbolic links (the counterpart of gekko's circuits). They connect several shoots; each shoot is a profile, a folder. Scattered fireflies echo the dots. No moon and no constellation, to avoid copying gekko.

## Deliverables

| Asset | Content |
| --- | --- |
| `master` | The validated single-figure illustration, on a flat background. |
| `avatar` | The master cut out, transparent background, 1024 px square. |
| `logo` | Flat, no text: the shell seen from above with its 7 dots, each with its lime halo (red disc, center line, black pronotum). Must hold at 32 and 16 px; if the 7 dots blur, a 3-dot variant. |
| `hero` | The twilight garden, no character (or very small). |
| `banner` | README banner (`.github/assets/banner.webp`, 1600 x 823): a wide scene generated by Codex from the master, the `og` card and the sibling banners, with the mascot standing in the garden (planted on a soil mound, overlapped by foreground grass, lit by the glowing links), cropped from the 16:9 original; the title and the tagline are composed afterwards as text. The only generated asset kept in the repository for now. |
| `project-card` / `og` | The character on the hero scene; the title and tagline are composed afterwards as text, never generated. |
| `sprites` | Animation sheets, 6 frames of 256 px in a row (1536 x 256), transparent, one scale for all sheets, see below. |
| `icons` | Favicon 32 and 64 px and a 180 px touch icon, cut from the logo. The 7 dots stay readable down to 32 px; at 16 px the icon reads as a red ladybug dot. |

### Sprites

| Sprite | Pose | Command |
| --- | --- | --- |
| `idle` | Breathes, antennae sway, the halos pulse. | rest |
| `link` | Carries a glowing lime thread from one shoot to another. | `install` |
| `guard` | Inspects with the magnifying glass. | `guard` |
| `fly` | Opens the elytra and flies from one side to the other. | `pull` |
| `secret` | Hides the key under a leaf. | `secrets` |

**v2 (smoother)**: every sprite also exists as a 12-frame sheet (3072 x 256, `sprites-v2/`): the 6 approved frames of v1 are kept as they are, with one generated in-between frame inserted after each (1, 1.5, 2 ... 6, 6.5; the last one leads back to frame 1). The playback duration stays the same, so each frame lasts half as long: about 62 ms (16 fps), and 150 ms for `secret`. The v1 sheets stay available (6 frames, `sprites/`). The site sets `--frames: 12` for v2.

## Constraints

- **No "bug" imagery**: never a pest, a magnifier over broken code, or a cross. The message is "it hunts bugs and leaks", not "it is a bug".
- **Ladybird**: checked against the Ladybird browser's repository (`UI/Icons/ladybird.png`): its current icon is an abstract mark (two interlocking ellipses on black), not a ladybug, so there is no conflict. The Ladybird children's book publisher does use a red and black ladybird, so the `dot` logo (the shell seen from above with seven black dots, each with a lime halo) must stay distinct from a plain red and black ladybird: the lime halos and the pronotum are what set it apart.
- **Legibility at small size**: if the 7 dots blur at 32 px, the logo keeps 3.
- **Not the Go gopher**: the project is written in Go, but the gopher is not ours to derive from.
- **Animations stoppable**: any animation on the site must be stoppable by the visitor.
- **No text in generated images**: titles and taglines are composed afterwards.

## Generation lessons

- **Single full-frame images, not contact sheets.** On a six-cell sheet each figure is about 400 px, so a "thick" outline turns into a thin line; the same prompt on one 1024 px figure gives the Gekko line weight.
- **For a retouch, pass the master alone as image 1** (given the Gekko avatar next to it, Codex returned the gecko). The prompts that worked say exhaustively what must stay identical, name what the previous attempt wrongly redrew (sneakers, eyelashes, blush, a nose, a glossy 3D look), and describe only the one thing to change. A prompt that describes the whole character without that keep-list invites a redraw.
- **Group the changes of a retouch into a single run**: each generation costs a drift.
- **Never pass a drifted sheet as a reference**: it propagates its flaws (eyelashes, orange-peel texture, a brown shell). For a new image pass the master and the finished Gekko and Shellkit illustrations.
- **Eyelashes and blush come back by default**: forbid them every time.
- **"Realistic" asked through volume and gloss drifts to a 3D vinyl toy; asked through grain it turns dirty.** Ask for the outline first, the painted semi-realistic shading second.
- **Transparency**: generate on a flat `#FF00FF` background, key it out and despill the edge. The generated "magenta" is never exactly `#FF00FF`: estimate the background from the image border before keying.
- **The shell drifts to one side, and grows**: say it is centered behind the torso, visible on both sides, symmetric, **small** (about 15 percent of the torso's width beyond it on each side), and that the split between the two elytra is in the middle of the back, hidden by the torso, not a seam on the side.
- **Halos tint the shell**: wide lime halos turn the red shell orange (measured on the red pixels: median hue about 15 degrees against about 5 for the validated red). Ask for a thin, tight aura. What is left can be fixed without the generator: compress the hue (0 to 40 degrees by a factor 0.45 at most, back to 1 at 40) of the bright saturated red pixels only (R at least 80 percent, saturation at least 0.6), which leaves the amber eyes, the gold objects, the cream and the lime halos untouched.
- **A stylized hero needs stylized references**: asked from a loose brief, the hero came out as a photoreal macro render; passing only the Gekko hero and forbidding photographic depth of field fixed it.
- **Sprites: let Codex draw, do the keying yourself.** Asked to key the background and assemble the 1536 x 256 sheet itself, Codex recolored the character (black and green instead of red and cream), left the magenta in place, or cut wide poses (flight, a leaf, a thread). What works: one image per sprite, a 3 x 2 grid of the 6 frames on a flat magenta background (nothing near a cell border, no script, no transparency), then cut, key, scale all frames by the same factor and align the bottoms with a local script, and check the result with `sprite-check.py`.
- **Check each frame at full resolution** (crop the heads from the original grid, a 256 px composite hides lashes and blush) for the lower arm pair, eyelashes, blush, a nose, the shell position (small, centered, no seam on the side), the head size, the red antenna beads and the gold of the key (despill makes them pale or pink).
- **A new sprite generated from the master alone drifts to chibi** (head about 50 percent of the height, a very short torso, thin legs, huge feet), even with a good sprite as a second reference. What keeps the proportions: derive the new grid from the approved `idle` grid as the only image, list exhaustively what must stay identical, and describe only the change (a different arm, different legs, a thread).
- **A readable story needs the object to leave the hand**: for `secret`, 6 steps (key raised, lowered, slid under the leaf, hand empty and key hidden, finger to the lips, key taken back so the loop closes). Play this one slowly (about 300 ms per frame, 1.8 s per loop).
- **A trailing thread must trail**: a long wavy open cord leaving the hand reads as a thread; a short one rises as a small curl, and a closed loop reads as a ring. A long cord makes the sheet wider than the character; let the sheet go up to about 238 px wide and the character shrink a few percent rather than cutting the cord.
- **Soft lime glows on magenta need their own unmixing**: the generic despill leaves a pink or orange veil on a thin glowing thread. Model each pixel as a mix of lime and magenta, and apply it only more than about 12 px away from the body so the cream and black edges of the character stay untouched. Segment the grid by connected components (the 6 large figures, small pieces going to the nearest one) so that a thread crossing into the neighbouring cell is not cut.
- **A strict side profile drifts the face** (a pointed muzzle, a nose, lashes, blush); keep the three-quarter view of the master for every sprite, flight included.
- **Smoother animation = in-betweens, not a longer grid.** A 12-cell grid would shrink every figure (thin outline). Instead, give Codex the approved 6-frame grid as the only image and ask for a 3 x 2 grid of the frames *halfway* between consecutive ones (cell 6 between frame 6 and frame 1); say that each new cell is a near-copy of the frame it follows with only the moving parts moved by half a step, and give the keep-list. Interleave locally (`sheet2.py`). Run all sprites in parallel and compare each result with its source grid before using it.
- **In-betweens can still redraw the character**: for `guard` and `secret` the first run returned a glossy 3D toy (pink cheeks, red nose, red shoes, purple lens, close-up on a leaf). Adding a paragraph that names exactly what the failed attempt drew wrong fixed it. Some cells can be picked from two runs (for `secret`, cell 4 from the second run, which kept the character standing like its neighbours); cut them by 512 px cell.
- **Interleaving**: scale the original frames with the v1 factor and the in-betweens with one factor (median of height ratio against the neighbours; fixed to 1 for `secret`, where crouching changes the height), then shrink everything by the same factor if the sheet spec (tallest 240, widest 224, link 238) would be exceeded. Anchor the body, not the bounding box, on one x for all frames (centroid of the opened mask, thread and antennae removed), or a growing thread pushes the body sideways by 30 px.
- **Scale**: normalize all sprite sheets with the same scale factor, not each sheet on its own tallest frame, or the character changes size from one sprite to the next.

## Decisions

- Name: Dotty, internal only.
- Accessories: magnifying glass and key.
- Red: pure red `#E5322D`.
- Biped on two simple legs, four arms.
- Shell centered behind the torso, small, split between the elytra in the middle of the back (not on the side); every one of the 7 dots has a thin lime halo (no single focus on the back).
- Runner-up considered: axolotl, dropped as too widely used and unrelated to the name. May reappear as a secondary illustration (profiles page), never as the project identity.
