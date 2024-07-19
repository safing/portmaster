#!/bin/sh

# Traymenu icons. Sometimes to wrong size is selected so remove 256 and 64.
convert pm_dark_green_512.png -colors 256 -define icon:auto-resize=48,32,16 pm_dark_green.ico
convert pm_dark_blue_512.png -colors 256 -define icon:auto-resize=48,32,16 pm_dark_blue.ico
convert pm_dark_red_512.png -colors 256 -define icon:auto-resize=48,32,16 pm_dark_red.ico
convert pm_dark_yellow_512.png -colors 256 -define icon:auto-resize=48,32,16 pm_dark_yellow.ico
convert pm_light_blue_512.png -colors 256 -define icon:auto-resize=48,32,16 pm_light_blue.ico
convert pm_light_green_512.png -colors 256 -define icon:auto-resize=48,32,16 pm_light_green.ico
convert pm_light_red_512.png -colors 256 -define icon:auto-resize=48,32,16 pm_light_red.ico
convert pm_light_yellow_512.png -colors 256 -define icon:auto-resize=48,32,16 pm_light_yellow.ico

convert pm_dark_512.png -colors 256 -define icon:auto-resize=64,48,32,16 pm_dark.ico
convert pm_light_512.png -colors 256 -define icon:auto-resize=64,48,32,16 pm_light.ico
