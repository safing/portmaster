#!/bin/sh

# Traymenu icons. Sometimes the wrong size is selected, so leave just one.
convert pm_dark_green_512.png -resize 64x64 pm_dark_green_64.png
convert pm_dark_blue_512.png -resize 64x64 pm_dark_blue_64.png
convert pm_dark_red_512.png -resize 64x64 pm_dark_red_64.png
convert pm_dark_yellow_512.png -resize 64x64 pm_dark_yellow_64.png
convert pm_light_blue_512.png -resize 64x64 pm_light_blue_64.png
convert pm_light_green_512.png -resize 64x64 pm_light_green_64.png
convert pm_light_red_512.png -resize 64x64 pm_light_red_64.png
convert pm_light_yellow_512.png -resize 64x64 pm_light_yellow_64.png

convert pm_dark_512.png -colors 256 -define icon:auto-resize=64,48,32,16 pm_dark.ico
convert pm_light_512.png -colors 256 -define icon:auto-resize=64,48,32,16 pm_light.ico
