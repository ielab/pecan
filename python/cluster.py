# -*- coding: utf-8 -*-
"""
Created on Thu Jan  7 16:40:15 2021

@author: Doly
"""

import sys
from numpy import unique
from numpy import where
from matplotlib import pyplot
import matplotlib.pyplot as plt
from sklearn.cluster import KMeans
from sklearn.decomposition import LatentDirichletAllocation
from sklearn.feature_extraction.text import TfidfVectorizer
import pandas as pd

def main(argv):
    messages = argv[0].split("§")
    vectorizer = TfidfVectorizer()
    X = vectorizer.fit_transform(messages)
    true_k = 2
    model = KMeans(n_clusters=true_k, init='k-means++', max_iter=200, n_init=10)
    model.fit(X)
    labels=model.labels_
    df=pd.DataFrame(list(zip(messages,labels)),columns=['title','cluster'])
    
    for i in range(len(df)):
        if df.iloc[i,0] == argv[1]:
            for index in df[df['cluster'] == df.iloc[i,1]].index:
                if df['title'][index]!="":
                    print(index)
    
if __name__ == "__main__":
   main(sys.argv[1:])