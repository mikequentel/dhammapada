# How and why the dhammapada X bot was created

## About and why

The **dhammapada** X bot posts entries from _The Dhammapada_ to X at: [portablebuddha](https://x.com/portablebuddha)

Because of my general interest in philosophy and religions, and especially my more recent reading about Buddhism, I determined that _The Dhammapada_, by F Max Müller, would be an opportunity to do the following:

1. Learn more about a philosophy I was not very familiar with and that I find interesting. I ended up reading the verses as I assembled them.
2. Create an automated solution that posts readings from an important scripture of this philosophy, combining a verse with a relevant image.
3. Use a source work that is relatively small in size and publicly available (hence, the _The Dhammapada_, with only about 400 verses).

The solution uses GitHub Actions to automate the call of the Go program `poster` to post via the X.com API a random verse and an accompanying image, at an X.com account named [portablebuddha](https://x.com/portablebuddha) (the name has "portable", in that a user can, via X.com, view verses from a mobile device or desktop).

## How the dhammapada X bot was created

### Collaboration with ChatGPT-5

Along the process of planning and development, I consulted ChatGPT-5 for feedback, suggestions, and code examples. As expected, there were things that were agreed and disagreed upon concerning the implementation. This is where responsible and discerning use of a generative AI can be a force multiplier. It generated most of the code, but much had to be refactored according to my personal preferences and judgement. In other words, it would have been foolish to just accept everything it suggested, as-is. Also, I am very particular about the layout and organisation of details in source code.

### Gaining access to the X API

At X, I performed the following:

1. I created a unique X account for posting the verses and images.
2. I [registered a Developer App](https://developer.x.com/en/portal/dashboard) so that the API could be used.

### GitHub project setup

In GitHub, I created the following:

1. Repository [dhammapada](https://github.com/mikequentel/dhammapada) to hold the proof-of-concept.
2. Private project _buddha_ to manage tasks, with the first task being _dhammapada X poster_
3. Relevant X API secrets variables in GitHub Actions.

### Acquisition of verses and images

In [archive.org](https://archive.org) I found a Public Domain copy of _The Dhammapada_, by F Max Müller and used the text to assemble the verses. Because of the strange, inconsistent, or difficult punctuation in the text, as I entered verses into a spreadsheet, I modified the punctuation so that it made sense according to the way I like to write. The verses were then exported to a CSV file.

The images were gathered from [WikiMedia Commons](https://commons.wikimedia.org) and water-marked with any relevant licensing information (eg: "CC BY-SA 3.0") and the source URL (so anyone could look up the image). Also, I included some of my own images.

The CSV file was then imported into SQLite database to make querying easier. Using the verse label, the bot can determine the corresponding image to post. There is no special significance or relationship between each verse and image; the image just happens to be "Buddhist-related", and is meant to make the post more interesting.

## Conclusion

This project has been an enlightening exercise in automating X posts, using one of my favourite programming languages (Go), learning more about another philosophy (Buddhism), and creating something entertaining and useful. I did this for the fun of it.
