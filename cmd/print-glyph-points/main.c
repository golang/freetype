/*
gcc main.c -I/usr/include/freetype2 -lfreetype && ./a.out
*/

#include <stdio.h>
#include <ft2build.h>
#include FT_FREETYPE_H

static int font_size = 12;
static int no_hinting = 0;

int main(int argc, char** argv) {
	FT_Error error;
	FT_Library library;
	FT_Face face;
	FT_Outline* o;
	int i, j;

	error = FT_Init_FreeType(&library);
	if (error) {
		printf("FT_Init_FreeType: error #%d\n", error);
		return 1;
	}
	error = FT_New_Face(library, "../../luxi-fonts/luxisr.ttf", 0, &face);
	if (error) {
		printf("FT_New_Face: error #%d\n", error);
		return 1;
	}
	error = FT_Set_Char_Size(face, 0, font_size*64, 0, 0);
	if (error) {
		printf("FT_Set_Char_Size: error #%d\n", error);
		return 1;
	}
	for (i = 0; i < face->num_glyphs; i++) {
		error = FT_Load_Glyph(face, i, no_hinting ? FT_LOAD_NO_HINTING : FT_LOAD_DEFAULT);
		if (error) {
			printf("FT_Load_Glyph: glyph %d: error #%d\n", i, error);
			return 1;
		}
		if (face->glyph->format != FT_GLYPH_FORMAT_OUTLINE) {
			printf("glyph format for glyph %d is not FT_GLYPH_FORMAT_OUTLINE\n", i);
			return 1;
		}
		o = &face->glyph->outline;
		for (j = 0; j < o->n_points; j++) {
			if (j != 0) {
				printf(", ");
			}
			printf("%ld %ld %d", o->points[j].x, o->points[j].y, o->tags[j] & 0x01);
		}
		printf("\n");
	}
}
