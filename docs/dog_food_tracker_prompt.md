# Dog Feeding and Health Tracking System Build - 'PupCup'

We are building a new, blank-slate multifaceted project called 'PupCup'. The purpose of this project is to create a system for tracking the feeding of dogs, along with their health. At its core, it is a web-based data record and analytics application. A core component of what makes this system special is a fast 'press and go' client device that allows users to record when and how well dogs eat with the press of a button. As such, this project contains several elements - both embedded linux, web application, and data analytics.

## Features

1. Track the feeding and health of up to 5 separate dogs. My household has 3 dogs - two are my girlfriend's and one is mine. But the app should allow a configurable number of named dogs with specific data tracked for each. The application will enable per-app tracking.
2. Simple data recording in web-application. (See 'recorded data' heading for specifics). In addition to button presses on the client device to record feeding, the user can log into a web application to record more specific details of the dog's feeding and health. 
3. Unified hardware device. In this device, the hardware that serves as the client device will also directly serve the web interface on the local network over wifi. We are using a Raspberry Pi 2W running the Pi version of Debian 13. This pi will be both the web server and the hardware client for a highly unified and simple system.
4. Pleasant, minimal UI. The web application needs to be highly intuitive while also being attractive. It needs to be easily hosted via the Raspberry Pi Zero 2W. The UI should have a fun and playful design, suitable for pet owners. The UI only needs to be accessible on the local network - but preferably should have an easy accessed url like 'pupcups.local' in addition to the IP.
5. Data analytics. One of the major purposes of this project is to help associate feeding habits with dog health. If a dog refuses to eat and doesn't feel well, it may be related to how, when, and what they ate. It is also useful to have more detailed records about the dog's health and feeding for vets. For example, the application will have records of when a dog doesn't feel well, and over time should be able to find potential trends in the causal factors. A dog might also eat better at a certain time than others, and users might be able to pin down the best time to feed their dogs with that data. Other analytics suggestions are welcome.
6. Simple 'were the dogs fed yet?' indicator. In a household with multiple dogs and different schedules, it is nice to know if the dogs were fed and how each dog ate. Therefore, the purpose of the client device is partly simply to indicate to other household members something like 'yes the dogs ate; Bard and Bentley ate well, Riley only ate a little'. It will do this with the client device. (See 'Client Device' for more details.)

## Recorded Data

1. Timestamp for each feeding, per dog. (Can be input with button or via web app after the fact; time stamps should be editable). (MANDATORY PER FEEDING)
2. Type of feeding: Two types - 'standard' (the dog's usual food with minimal modifications) and 'nonstandard' (different food than usual or the usual food with significant add-ins). (MANDATORY PER FEEDING - should automatically be entered as 'standard', but can be modified as 'nonstandard' via the web interface)
3. 'How well did they eat' rating: This corresponds to buttons on the physical device which are green, yellow, and red. These can be tracked in data by 'Fully Consumed', 'Partially Consumed', or 'Not Consumed'. Tracked per-dog. (MANDATORY PER FEEDING)
4. (OPTIONAL PER FEEDING) List of specifics for what was fed, entered via web application
5. (OPTIONAL PER DAY) If the dog was sick or unhealthy on a particular day, the user can input that via the web application.
6. (OPTIONAL, anytime) Snacks or treats, recorded with timestamp 
7. (OPTIONAL, anytime) "Potentially stressful circumstances" with notes on specifics. Denotes if a dog is traveling, being watched by someone else, has a large number of strange houseguests, etc.)

## Client Device

The core purpose of the 'client device' (which is actually integrated into the same Raspberry Pi Zero 2W that serves the web application) is to be a simplified, fast way to 1) Record when dogs eat their meals and how well they ate with simple button presses and 2) display if and how well the dogs ate via RGB LED lights and a small SSD1306 OLED. 

The client device interface works as following. 

1. The core input is a series of 4 different colored buttons - GREEN, YELLOW, RED, and BLUE. These correspond to 'GREEN: Ate full meal.', 'YELLOW: Ate partial meal, or ate meal over time', RED: Didn't eat when given food', and 'BLUE: Ate a snack'. 
2. There is also a KY-040 rotary encoder for navigation of the screen to select the dog for whom the buttons are recording a meal and to review the most recent feeding.
3. The process to record a meal or snack is as follows: 1) The SSD 1306 displays a page for each dog. The dog's name is in large, easily-readable text. The purpose of this page is so the user knows which dog is selected for input. As the user rotates the dial, it scrolls through the pages of dogs, one page for each dog. 2) After the user rotates the dial to the dog they want to record the meal for, with that dog's name displayed on screen, the user presses on of the four buttons (GREEN, YELLOW, RED, or BLUE). This automatically creates a record of the time when pressed and which button was pressed (or how well the dog ate). 3) The user then rotates the dial to the next dog name, and presses the appropriate button. This process repeats until the records of the meal are all recorded. 4) For the next 4 hours after meals are recorded for all dogs, the client device is locked from creating new meal entries with buttons (unless overridden with a long press of the rotary encoder, which allows the device to record another meal). This prevents accidental inputs. During this four hour period, the SSD1306 OLED will not show the 'one name per page' UI. Instead, it will have a single page listing all dogs, with a record of how well they ate at the previous meal next to their name. 5) For the four hour period after feeding, an LED strip on the display will glow green to indicate that the dogs were fed (this is so other members of the household know the dogs were fed). This only displays after meals, not snacks. 6) A snack (recorded via the blue button) can be recorded for any dog at any time, even during the locked post-meal period. This is done by pressing and holding the snack button. Then the page of dog names comes up just like with a meal. The user then records a snack by pressing the blue button for each dog that ate a snack (selected with the rotary dial). The 'snack recording mode' ends automatically after either all 3 dogs are recorded or after 1 minute, when it reverts back to the previous state.

## Hardware
- Raspberry Pi Zero 2W, mounted on perfboard
- SSD 1306 OLED Display (0.91")
- Rotary Encoder - KY-040
- Adafruit 8 NeoPixel Stick with SKC6812 
- Capacitors and Resistors as required
- USB-C PD Trigger at 5V

## Tech Stack
- Pure Go for web application and client device
- bbolt for data storage

## Future Features
These are features that will be added eventually to expand the core functionality. Room should be left in the code for these features to be added. 
- Data analysis to find trends in data
- Home Assistant integration to display recent meals on Home Assistant dashboard
